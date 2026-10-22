#!/usr/bin/env python3
"""Isolated CodeFormer + ArcFace gate. Runs in .venv-codeformer."""
import argparse
import json
import os
from pathlib import Path
import sys

# The official CodeFormer checkout is the working directory. Prefer its
# vendored BasicSR, which contains the MPS-aware get_device helper.
sys.path.insert(0,os.getcwd())

import cv2
import numpy as np
import torch
import torch.nn.functional as F
from torchvision.transforms.functional import normalize

from basicsr.utils import img2tensor, tensor2img
from basicsr.utils.misc import get_device
from basicsr.utils.registry import ARCH_REGISTRY
from facexlib.recognition.arcface_arch import Backbone
from facelib.utils.face_restoration_helper import FaceRestoreHelper


def sharpness(bgr):
    gray=cv2.cvtColor(cv2.resize(bgr,(512,512),interpolation=cv2.INTER_AREA),cv2.COLOR_BGR2GRAY)
    return float(cv2.Laplacian(gray.astype(np.float32)/255,cv2.CV_32F,ksize=3).var())


def clipped(bgr):
    return float(np.mean(np.max(bgr,axis=2)>=254))


def embedding(model,bgr,device):
    rgb=cv2.cvtColor(cv2.resize(bgr,(112,112),interpolation=cv2.INTER_AREA),cv2.COLOR_BGR2RGB)
    tensor=torch.from_numpy(rgb.transpose(2,0,1).copy()).float().unsqueeze(0).to(device)/127.5-1
    with torch.no_grad(): value=model(tensor)
    return F.normalize(value.float(),dim=1)


def restore(net,bgr,fidelity,device):
    tensor=img2tensor(bgr/255.,bgr2rgb=True,float32=True)
    normalize(tensor,(.5,.5,.5),(.5,.5,.5),inplace=True)
    with torch.no_grad(): output=net(tensor.unsqueeze(0).to(device),w=float(fidelity),adain=True)[0]
    return tensor2img(output,rgb2bgr=True,min_max=(-1,1)).astype(np.uint8)


def detect_original_geometry(helper, image, resize=800, eye_dist_threshold=5):
    """Keep vendor detector preprocessing, map only geometry to original pixels.

    The vendor helper may enlarge its working image. Detection can use that
    image, but alignment and inverse paste must share the original canvas.
    Confidence is not a coordinate and must never be divided by resize.
    """
    if image is None or image.ndim != 3 or image.shape[2] != 3 or image.dtype != np.uint8:
        raise ValueError('CODEFORMER_BGR_UINT8_INPUT_REQUIRED')
    helper.detection_short_side = resize
    helper.read_image(image)
    working = helper.input_img
    h, w = working.shape[:2]
    scale = resize / min(h, w)
    dh, dw = int(h * scale), int(w * scale)
    detector_image = cv2.resize(working, (dw, dh),
        interpolation=cv2.INTER_AREA if scale < 1 else cv2.INTER_LINEAR)
    with torch.no_grad():
        boxes = helper.face_detector.detect_faces(detector_image)
    # All subsequent alignment/warp/paste operations use this same canvas.
    helper.input_img = image.copy()
    if boxes is None or len(boxes) == 0:
        return 0
    boxes = np.asarray(boxes).copy()
    if boxes.ndim != 2 or boxes.shape[1] != 15 or not np.isfinite(boxes).all():
        raise ValueError('CODEFORMER_DETECTOR_GEOMETRY_INVALID')
    oh, ow = image.shape[:2]
    # Exact realized dimensions account for integer rounding on each axis.
    boxes[:, [0, 2, 5, 7, 9, 11, 13]] *= ow / dw
    boxes[:, [1, 3, 6, 8, 10, 12, 14]] *= oh / dh
    for box in boxes:
        if box[2] <= box[0] or box[3] <= box[1] or not 0 <= box[4] <= 1:
            raise ValueError('CODEFORMER_DETECTOR_GEOMETRY_INVALID')
        eye_distance = np.linalg.norm(box[5:7] - box[7:9])
        if eye_dist_threshold is not None and eye_distance < eye_dist_threshold:
            continue
        helper.det_faces.append(box[:5])
        helper.all_landmarks_5.append(box[5:].reshape(5, 2))
    return len(helper.all_landmarks_5)


def face_geometry_evidence(helper, index):
    """Detector geometry only, in original input pixels; no identity assignment."""
    box = helper.det_faces[index]
    landmarks = helper.all_landmarks_5[index]
    return {'detector_box_original_xyxy': [float(v) for v in box[:4]],
            'detector_confidence': float(box[4]),
            'detector_landmarks_original_xy': [[float(x), float(y)] for x, y in landmarks]}


def main():
    ap=argparse.ArgumentParser();ap.add_argument('--request',required=True);ap.add_argument('--response',required=True)
    args=ap.parse_args(); request=json.loads(Path(args.request).read_text())
    device=get_device()
    net=ARCH_REGISTRY.get('CodeFormer')(dim_embd=512,codebook_size=1024,n_head=8,n_layers=9,
        connect_list=['32','64','128','256']).to(device)
    state=torch.load(request['codeformer_model'],map_location='cpu')['params_ema']
    net.load_state_dict(state);net.eval()
    arc=Backbone(num_layers=50,drop_ratio=.6,mode='ir_se').to(device)
    arc.load_state_dict(torch.load(request['arcface_model'],map_location='cpu'));arc.eval()
    helper=FaceRestoreHelper(1,face_size=512,crop_ratio=(1,1),det_model='retinaface_resnet50',
                             save_ext='png',use_parse=True,device=device)
    results=[]
    for spec in request['images']:
        image=cv2.imread(spec['input'],cv2.IMREAD_COLOR)
        helper.clean_all()
        detected=detect_original_geometry(helper,image,eye_dist_threshold=5)
        helper.align_warp_face(); faces=[]; accepted=0
        if not (len(helper.cropped_faces) == len(helper.det_faces) == len(helper.all_landmarks_5) == detected):
            raise RuntimeError('CODEFORMER_FACE_GEOMETRY_CARDINALITY')
        for index,original in enumerate(helper.cropped_faces):
            geometry=face_geometry_evidence(helper,index)
            base_score=sharpness(original); base_clip=clipped(original)
            if base_score>=request['threshold']:
                helper.add_restored_face(original,original)
                faces.append({**geometry,'index':index,'status':'sharp_skip','sharpness_before':base_score})
                continue
            base_emb=embedding(arc,original,device); candidates=[]
            for fidelity in request['fidelity_candidates']:
                candidate=restore(net,original,fidelity,device)
                similarity=float((base_emb*embedding(arc,candidate,device)).sum())
                score=sharpness(candidate); clip=clipped(candidate)
                candidates.append({'fidelity':fidelity,'image':candidate,'identity_similarity':similarity,
                                   'sharpness':score,'clipped_fraction':clip})
            valid=[x for x in candidates if x['identity_similarity']>=request['min_identity_similarity']
                   and x['sharpness']>base_score*1.05 and x['clipped_fraction']<=base_clip+.02]
            valid.sort(key=lambda x:(x['sharpness'],x['identity_similarity']),reverse=True)
            chosen=None
            for candidate in valid:
                blended=cv2.addWeighted(original,1-request['blend'],candidate['image'],request['blend'],0)
                blend_similarity=float((base_emb*embedding(arc,blended,device)).sum())
                if blend_similarity>=request['min_identity_similarity']:
                    chosen=(candidate,blended,blend_similarity);break
            if chosen:
                candidate,face,blend_similarity=chosen;accepted+=1
                helper.add_restored_face(face,original)
                faces.append({**geometry,'index':index,'status':'accepted','fidelity':candidate['fidelity'],
                    'identity_similarity':candidate['identity_similarity'],'blend_identity_similarity':blend_similarity,
                    'sharpness_before':base_score,'sharpness_candidate':candidate['sharpness'],
                    'sharpness_after':sharpness(face)})
            else:
                helper.add_restored_face(original,original)
                faces.append({**geometry,'index':index,'status':'rejected','sharpness_before':base_score,
                    'candidates':[{k:v for k,v in x.items() if k!='image'} for x in candidates]})
        output=Path(spec['output']);output.parent.mkdir(parents=True,exist_ok=True)
        if accepted:
            helper.get_inverse_affine(None); final=helper.paste_faces_to_input_image()
        else: final=image
        if final.shape != image.shape:
            raise RuntimeError('CODEFORMER_OUTPUT_GEOMETRY_CHANGED')
        if not cv2.imwrite(str(output),final):
            raise RuntimeError('CODEFORMER_OUTPUT_WRITE_FAILED')
        results.append({'source_id':spec['source_id'],'relative':spec['relative'],'output':str(output),
                        'detector_short_side':int(helper.detection_short_side),
                        'coordinate_space':'original_image_pixels',
                        'input_width':int(image.shape[1]),'input_height':int(image.shape[0]),
                        'detected_faces':int(detected),'accepted_faces':accepted,'faces':faces})
    Path(args.response).write_text(json.dumps({'images':results},ensure_ascii=False,indent=2)+'\n')


if __name__=='__main__': main()
