import cv2
import numpy as np
from .models import model_dir

# MediaPipe face topology: preserve eye/brow/lip/nose areas from smoothing and warp.
EYES = [[33,160,158,133,153,144], [362,385,387,263,373,380]]
BROWS = [[70,63,105,66,107,55,65,52,53,46], [336,296,334,293,300,276,283,282,295,285]]
MOUTH = [61,40,37,0,267,270,291,321,314,17,84,91]
OVAL = [10,338,297,332,284,251,389,356,454,323,361,288,397,365,379,378,400,377,152,148,176,149,150,136,172,58,132,93,234,127,162,21,54,103,67,109]


def luma(rgb):
    return rgb[...,0]*.2126 + rgb[...,1]*.7152 + rgb[...,2]*.0722


def linear(rgb):
    return np.where(rgb <= .04045, rgb/12.92, ((rgb+.055)/1.055)**2.4).astype(np.float32)


def encoded(rgb):
    return np.where(rgb <= .0031308, rgb*12.92, 1.055*np.maximum(rgb, 0)**(1/2.4)-.055).astype(np.float32)


def polygon(shape, points, indices):
    mask = np.zeros(shape, np.float32)
    pts = np.round(points[indices]).astype(np.int32)
    cv2.fillConvexPoly(mask, cv2.convexHull(pts), 1.)
    return mask


def blur(mask, sigma):
    return cv2.GaussianBlur(mask, (0,0), max(.5, sigma))


class Analyzer:
    def __init__(self):
        import mediapipe as mp
        self.mp = mp
        base = mp.tasks.BaseOptions
        vision = mp.tasks.vision
        self.faces = vision.FaceLandmarker.create_from_options(vision.FaceLandmarkerOptions(
            base_options=base(model_asset_path=str(model_dir()/'face_landmarker.task'), delegate=base.Delegate.CPU),
            num_faces=12, min_face_detection_confidence=.35, min_face_presence_confidence=.4))
        self.segmenter = vision.ImageSegmenter.create_from_options(vision.ImageSegmenterOptions(
            base_options=base(model_asset_path=str(model_dir()/'selfie_multiclass.tflite'), delegate=base.Delegate.CPU),
            output_confidence_masks=True, output_category_mask=False))

    def close(self):
        self.faces.close()
        self.segmenter.close()

    def image(self, rgb):
        return self.mp.Image(image_format=self.mp.ImageFormat.SRGB, data=np.ascontiguousarray(rgb))

    def analyze(self, rgb):
        h, w = rgb.shape[:2]
        f = rgb.astype(np.float32)/255
        y = luma(f)
        # Detection-only shadow boost; final pipeline always uses original pixels.
        gain = min(2.2, .45 / max(.12, float(np.median(y))))
        detect_rgb = np.uint8(np.clip(f*gain, 0, 1)*255)
        masks = self.segmenter.segment(self.image(detect_rgb)).confidence_masks
        probs = [cv2.resize(m.numpy_view().copy(), (w,h)) for m in masks]
        face_skin = np.clip((probs[3]-.35)/.55, 0, 1)
        body_skin = np.clip((probs[2]-.4)/.5, 0, 1)
        person = np.clip(1-probs[0], 0, 1)
        detected = []
        # Whole-frame + overlapping crops: short-range detector misses small family faces.
        windows = [(0,0,w,h)]
        for top in [0, int(h*.25)]:
            for left in [0, int(w*.25), int(w*.5)]:
                windows.append((left, top, min(w,left+int(w*.5)), min(h,top+int(h*.65))))
        # Smaller overlapping windows cover full-length group portraits.
        size = max(240, int(w*.28))
        stride = max(120, int(size*.65))
        for top in range(0, max(1,h-size+1), stride):
            for left in range(0, max(1,w-size+1), stride):
                windows.append((left,top,min(w,left+size),min(h,top+size)))
        for x0,y0,x1,y1 in windows:
            result = self.faces.detect(self.image(detect_rgb[y0:y1,x0:x1]))
            for landmarks in result.face_landmarks:
                pts = np.array([[p.x*(x1-x0)+x0,p.y*(y1-y0)+y0] for p in landmarks], np.float32)
                lo, hi = pts.min(axis=0), pts.max(axis=0)
                center = (lo+hi)/2
                width = float(hi[0]-lo[0])
                if width < 20 or np.any(lo < 0) or hi[0] >= w or hi[1] >= h:
                    continue
                if any(np.linalg.norm(center-d['center']) < max(width, d['width'])*.45 for d in detected):
                    continue
                detected.append({'points':pts, 'center':center, 'width':width})
        detected.sort(key=lambda d:float(d['center'][0]))
        face_masks, protective, faces = [], np.zeros((h,w),np.float32), []
        all_oval = np.zeros((h,w), np.float32)
        for i,d in enumerate(detected):
            p, fw = d['points'], d['width']
            oval = polygon((h,w),p,OVAL)
            all_oval = np.maximum(all_oval,oval)
            protected = np.zeros((h,w), np.float32)
            for poly in EYES+BROWS+[MOUTH,[1,2,98,327]]:
                protected = np.maximum(protected, polygon((h,w),p,poly))
            protected = cv2.dilate(protected, np.ones((max(3,int(fw*.035))|1,)*2,np.uint8))
            protected = np.clip(blur(protected,fw*.015)*1.4,0,1)
            protective = np.maximum(protective,protected)
            mask = blur(oval,fw*.025)*face_skin*(1-protected)
            face_masks.append(mask)
            valid = mask>.5
            median = float(np.median(luma(linear(f))[valid])) if valid.sum()>30 else float(np.median(luma(linear(f))[oval>.5]))
            faces.append({'id':f'person-{i+1}', 'points':(p/[w,h]).tolist(),
                          'bbox':[float(p[:,0].min()/w),float(p[:,1].min()/h),float(p[:,0].max()/w),float(p[:,1].max()/h)],
                          'median_linear_luma':median, 'mask_pixels':int(valid.sum())})
        skin = np.maximum.reduce(face_masks) if face_masks else np.zeros((h,w),np.float32)
        body_skin *= (1-all_oval)*(1-protective)
        # Confidence refinement by image edges, preserving continuous masks.
        guide = y.astype(np.float32)
        skin = np.clip(cv2.ximgproc.guidedFilter(guide,skin,5,.005),0,1)*(1-protective)
        body_skin = np.clip(cv2.ximgproc.guidedFilter(guide,body_skin,5,.005),0,1)
        stats = {'faces':faces, 'face_count':len(faces),
                 'median_linear_luma':float(np.median(luma(linear(f)))),
                 'clipped_fraction':float(np.mean(np.max(f,axis=2)>=.995)),
                 'proxy_size':[w,h]}
        maps = {'face_skin':skin,'body_skin':body_skin,'person':person,'protected':protective,
                'face_oval':all_oval, 'luma':luma(linear(f))}
        return stats, maps
