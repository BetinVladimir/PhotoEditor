"""Offline factories for architecture IDs admitted by an immutable model lock."""
from __future__ import annotations
from pathlib import Path
import os
import numpy as np
from .install import ModelError

DIFFUSERS_ARCHITECTURE='diffusers.stable_diffusion_inpaint.v1'
LRASPP_ARCHITECTURE='torchvision.lraspp_mobilenet_v3_large.coco_voc_v1'
LRASPP_WEIGHT_ID='LRASPP_MobileNet_V3_Large_Weights.COCO_WITH_VOC_LABELS_V1'
KEYPOINT_ARCHITECTURE='torchvision.keypointrcnn_resnet50_fpn.coco_v1'
KEYPOINT_WEIGHT_ID='KeypointRCNN_ResNet50_FPN_Weights.COCO_V1'
SFD_ARCHITECTURE='torchscript.face_alignment.sfd.v1'
SFD_WEIGHT_ID='face_alignment.s3fd-619a316812.pth'
FAN_ARCHITECTURE='torchscript.face_alignment.fan2d.v1'
FAN_WEIGHT_ID='face_alignment.2DFAN4-cd938726ad.pth.tar'
DINO_ARCHITECTURE='transformers.dinov2_vits14.v1'
DEPTH_ARCHITECTURE='transformers.depth_anything_v2_small.v1'
DINO_WEIGHT_ID='facebook/dinov2-small'
DEPTH_WEIGHT_ID='depth-anything/Depth-Anything-V2-Small-hf'
SEGFORMER_ARCHITECTURE='transformers.segformer_b0_ade.v1'
SEGFORMER_WEIGHT_ID='nvidia/segformer-b0-finetuned-ade-512-512'
U2NET_ARCHITECTURE='onnx.u2net.v1'
U2NET_WEIGHT_ID='rembg/u2net.onnx'
FACE_PARSE_ARCHITECTURE='torchscript.bisenet_face19.v1'
FACE_PARSE_WEIGHT_ID='zllrunning/79999_iter.pth.torchscript'
MOBILESAM_ARCHITECTURE='torchscript.mobilesam_tinyvit.v1'
MOBILESAM_WEIGHT_ID='MobileSAM/mobile_sam.pt.torchscript'
IDENTITY512_ARCHITECTURE='onnx.identity512.v2_1'
IDENTITY512_WEIGHT_ID='identity512_onnx_v2_1'

def _device(backend):
    import torch
    if backend=='cuda' and not torch.cuda.is_available():raise ModelError('BACKEND_UNAVAILABLE')
    if backend=='mps' and not torch.backends.mps.is_available():raise ModelError('BACKEND_UNAVAILABLE')
    return torch.device(backend)

def _single_weight(files):
    candidates=[path for name,path in files.items() if Path(name).suffix.lower() in ('.pt','.pth','.torchscript')]
    if len(candidates)!=1:raise ModelError('MODEL_LAYOUT_INVALID')
    return candidates[0]

class TorchScriptRGB:
    def __init__(self,path,backend):
        import torch
        self.torch=torch;self.device=_device(backend)
        try:self.model=torch.jit.load(str(path),map_location=self.device).eval()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    def _tensor(self,rgb):return self.torch.from_numpy(np.ascontiguousarray(rgb.transpose(2,0,1)[None])).to(self.device)
    def _result(self,value):
        if isinstance(value,(tuple,list)):value=value[0]
        return value.detach().to('cpu',dtype=self.torch.float32).numpy()[0].transpose(1,2,0)
    def infer(self,rgb,*args):
        with self.torch.inference_mode():
            if args:
                mask=self.torch.from_numpy(np.ascontiguousarray(args[0][None,None])).to(self.device)
                return self._result(self.model(self._tensor(rgb),mask))
            return self._result(self.model(self._tensor(rgb)))

class DiffusersInpaint:
    def __init__(self,root,backend):
        import torch
        try:
            from diffusers import StableDiffusionInpaintPipeline,DDIMScheduler
            dtype=torch.float16 if backend in ('cuda','mps') else torch.float32
            self.pipe=StableDiffusionInpaintPipeline.from_pretrained(str(root),local_files_only=True,torch_dtype=dtype,safety_checker=None,requires_safety_checker=False)
            self.pipe.scheduler=DDIMScheduler.from_config(self.pipe.scheduler.config);self.pipe.to(_device(backend));self.torch=torch;self.device=backend
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    def generate(self,rgb,mask,*,prompt,negative_prompt,seed,steps,guidance_scale,scheduler):
        from PIL import Image
        if scheduler!='ddim_v1' or steps!=25:raise ValueError('GENERATION_CONFIG_INVALID')
        image=Image.fromarray(np.round(np.clip(rgb,0,1)*255).astype(np.uint8),'RGB')
        matte=Image.fromarray(np.where(mask>.5,255,0).astype(np.uint8),'L')
        generator=self.torch.Generator(device=self.device).manual_seed(seed)
        result=self.pipe(prompt=prompt,negative_prompt=negative_prompt,image=image,mask_image=matte,
            num_inference_steps=steps,guidance_scale=guidance_scale,generator=generator,height=512,width=512).images
        if len(result)!=1:raise ValueError('MODEL_OUTPUT_INVALID')
        return np.asarray(result[0],np.float32)/255

class TorchvisionLRASPP:
    def __init__(self,path,backend):
        import torch
        try:
            from torchvision.models.segmentation import lraspp_mobilenet_v3_large
            self.torch=torch;self.device=_device(backend)
            self.model=lraspp_mobilenet_v3_large(weights=None,weights_backbone=None,num_classes=21).to(self.device)
            state=torch.load(str(path),map_location='cpu',weights_only=True)
            if isinstance(state,dict) and set(state)=={'state_dict'}:state=state['state_dict']
            self.model.load_state_dict(state,strict=True);self.model.eval()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    def infer(self,normalized_rgb):
        value=np.asarray(normalized_rgb,np.float32)
        if value.ndim!=3 or value.shape[2]!=3 or not np.isfinite(value).all():raise ValueError('MODEL_INPUT_INVALID')
        tensor=self.torch.from_numpy(np.ascontiguousarray(value.transpose(2,0,1)[None])).to(self.device)
        with self.torch.inference_mode():result=self.model(tensor)
        if not isinstance(result,dict) or set(result)!={'out'}:raise ValueError('MODEL_OUTPUT_INVALID')
        return result['out'].detach().to('cpu',dtype=self.torch.float32).numpy()[0]

class TorchvisionKeypointRCNN:
    def __init__(self,path,backend):
        import torch
        try:
            from torchvision.models.detection import keypointrcnn_resnet50_fpn
            self.torch=torch;self.device=_device(backend)
            self.model=keypointrcnn_resnet50_fpn(weights=None,weights_backbone=None,num_classes=2,num_keypoints=17,
                min_size=640,max_size=1024).to(self.device)
            state=torch.load(str(path),map_location='cpu',weights_only=True)
            if isinstance(state,dict) and set(state)=={'state_dict'}:state=state['state_dict']
            self.model.load_state_dict(state,strict=True);self.model.eval()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    @staticmethod
    def _bounded_pose_fields(item):
        required={'boxes','scores','keypoints','keypoints_scores'}
        if not isinstance(item,dict) or not required<=set(item):
            raise ValueError('MODEL_OUTPUT_INVALID')
        values={name:item[name] for name in required}
        scores=values['scores']
        shape=getattr(scores,'shape',None)
        if shape is None or len(shape)!=1 or type(shape[0]) is not int or not 0<=shape[0]<=10000:
            raise ValueError('MODEL_OUTPUT_INVALID')
        count=shape[0]
        expected={'scores':(count,), 'boxes':(count,4),
                  'keypoints':(count,17,3), 'keypoints_scores':(count,17)}
        if any(getattr(value,'shape',None) is None or
               tuple(value.shape)!=expected[name] or
               not callable(getattr(value,'is_floating_point',None)) or
               not value.is_floating_point()
               for name,value in values.items()):
            raise ValueError('MODEL_OUTPUT_INVALID')
        return values
    def infer(self,rgb):
        value=np.asarray(rgb,np.float32)
        if value.ndim!=3 or value.shape[2]!=3 or not np.isfinite(value).all() or np.any(value<0) or np.any(value>1):raise ValueError('MODEL_INPUT_INVALID')
        tensor=self.torch.from_numpy(np.ascontiguousarray(value.transpose(2,0,1))).to(self.device)
        with self.torch.inference_mode():result=self.model([tensor])
        if not isinstance(result,list) or len(result)!=1 or not isinstance(result[0],dict):raise ValueError('MODEL_OUTPUT_INVALID')
        item=self._bounded_pose_fields(result[0])
        values={name:value.detach().to('cpu',dtype=self.torch.float32).numpy()
                for name,value in item.items()}
        return _pose_detection_fields(values)


def _pose_detection_fields(values):
    """Translate torchvision's plural score key to the Pose17 worker ABI."""
    if not isinstance(values,dict) or set(values)!={'boxes','scores','keypoints','keypoints_scores'}:
        raise ValueError('MODEL_OUTPUT_INVALID')
    return {'boxes':values['boxes'],'scores':values['scores'],
            'keypoints':values['keypoints'],'keypoint_scores':values['keypoints_scores']}

class TorchScriptSFD:
    def __init__(self,path,backend):
        import torch
        try:self.torch=torch;self.device=_device(backend);self.model=torch.jit.load(str(path),map_location=self.device).eval()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    @staticmethod
    def _bounded_output(value):
        shape=tuple(value.shape)
        if (not value.is_floating_point() or
                not (len(shape)==2 and shape[1]==5 and shape[0]<=100000 or
                     len(shape)==3 and shape[0]==1 and shape[2]==5 and shape[1]<=100000)):
            raise ValueError('MODEL_OUTPUT_INVALID')
        return value
    def infer(self,bgr_mean_subtracted):
        value=np.asarray(bgr_mean_subtracted,np.float32)
        if value.ndim!=3 or value.shape[2]!=3 or not np.isfinite(value).all():raise ValueError('MODEL_INPUT_INVALID')
        tensor=self.torch.from_numpy(np.ascontiguousarray(value.transpose(2,0,1)[None])).to(self.device)
        with self.torch.inference_mode():result=self.model(tensor)
        if isinstance(result,(tuple,list)) and len(result)==1:result=result[0]
        if not isinstance(result,self.torch.Tensor):raise ValueError('MODEL_OUTPUT_INVALID')
        result=self._bounded_output(result)
        output=result.detach().to('cpu',dtype=self.torch.float32).numpy()
        if output.ndim==3 and output.shape[0]==1:output=output[0]
        if output.ndim!=2 or output.shape[1]!=5:raise ValueError('MODEL_OUTPUT_INVALID')
        return output

class TorchScriptFAN:
    def __init__(self,path,backend):
        import torch
        try:self.torch=torch;self.device=_device(backend);self.model=torch.jit.load(str(path),map_location=self.device).eval()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    @staticmethod
    def _bounded_output(value):
        if (tuple(value.shape) not in ((68,3),(1,68,3)) or
                not value.is_floating_point()):
            raise ValueError('MODEL_OUTPUT_INVALID')
        return value
    def infer(self,crop_rgb):
        value=np.asarray(crop_rgb,np.float32)
        if value.shape!=(256,256,3) or not np.isfinite(value).all() or np.any(value<0) or np.any(value>1):raise ValueError('MODEL_INPUT_INVALID')
        tensor=self.torch.from_numpy(np.ascontiguousarray(value.transpose(2,0,1)[None])).to(self.device)
        with self.torch.inference_mode():result=self.model(tensor)
        if isinstance(result,(tuple,list)) and len(result)==1:result=result[0]
        if not isinstance(result,self.torch.Tensor):raise ValueError('MODEL_OUTPUT_INVALID')
        result=self._bounded_output(result)
        output=result.detach().to('cpu',dtype=self.torch.float32).numpy()
        if output.shape==(1,68,3):output=output[0]
        if output.shape!=(68,3):raise ValueError('MODEL_OUTPUT_INVALID')
        return output

class TransformersVision:
    def __init__(self,root,backend,kind):
        import torch
        try:
            from transformers import AutoImageProcessor,AutoModel,AutoModelForDepthEstimation
            self.torch=torch;self.device=_device(backend);self.processor=AutoImageProcessor.from_pretrained(str(root),local_files_only=True)
            cls=AutoModel if kind=='dino' else AutoModelForDepthEstimation
            self.model=cls.from_pretrained(str(root),local_files_only=True).to(self.device).eval();self.kind=kind
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc

    @staticmethod
    def _bounded_depth_output(value):
        # Validate on-device metadata before detach/copy can allocate a second
        # full tensor in host memory. DepthAnything's pinned 518px processor
        # should be well below this conservative four-megapixel ceiling.
        shape=getattr(value,'shape',None)
        if (shape is None or len(shape)!=3 or shape[0]!=1 or
                type(shape[1]) is not int or type(shape[2]) is not int or
                shape[1]<1 or shape[2]<1 or shape[1]*shape[2]>4194304 or
                not callable(getattr(value,'is_floating_point',None)) or
                not value.is_floating_point()):
            raise ValueError('MODEL_OUTPUT_INVALID')
        return value

    @staticmethod
    def _bounded_dino_output(output):
        value=getattr(output,'pooler_output',None)
        if value is None:
            hidden=getattr(output,'last_hidden_state',None)
            shape=getattr(hidden,'shape',None)
            if (shape is None or len(shape)!=3 or shape[0]!=1 or
                    type(shape[1]) is not int or not 1<=shape[1]<=4096 or
                    shape[2]!=384 or
                    not callable(getattr(hidden,'is_floating_point',None)) or
                    not hidden.is_floating_point()):
                raise ValueError('MODEL_OUTPUT_INVALID')
            value=hidden[:,0]
        shape=getattr(value,'shape',None)
        if (shape is None or tuple(shape)!=(1,384) or
                not callable(getattr(value,'is_floating_point',None)) or
                not value.is_floating_point()):
            raise ValueError('MODEL_OUTPUT_INVALID')
        return value

    def infer(self,rgb):
        from PIL import Image
        value=np.asarray(rgb,np.float32)
        if value.ndim!=3 or value.shape[2]!=3 or not np.isfinite(value).all() or np.any(value<0) or np.any(value>1):raise ValueError('MODEL_INPUT_INVALID')
        image=Image.fromarray(np.rint(value*255).astype(np.uint8),'RGB');inputs=self.processor(images=image,return_tensors='pt')
        inputs={k:v.to(self.device) for k,v in inputs.items()}
        with self.torch.inference_mode():out=self.model(**inputs)
        if self.kind=='dino':
            value=self._bounded_dino_output(out)
            return value.detach().to('cpu',dtype=self.torch.float32).numpy()[0]
        value=self._bounded_depth_output(getattr(out,'predicted_depth',None))
        value=value.detach().to('cpu',dtype=self.torch.float32).numpy()
        return value[0]

class TransformersSegformer:
    def __init__(self,root,backend):
        import torch
        try:
            from transformers import AutoImageProcessor,SegformerForSemanticSegmentation
            self.torch=torch;self.device=_device(backend);self.processor=AutoImageProcessor.from_pretrained(str(root),local_files_only=True)
            self.model=SegformerForSemanticSegmentation.from_pretrained(str(root),local_files_only=True).to(self.device).eval()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    @staticmethod
    def _bounded_logits(value):
        shape=getattr(value,'shape',None)
        if (shape is None or len(shape)!=4 or shape[0]!=1 or shape[1]!=150 or
                type(shape[2]) is not int or type(shape[3]) is not int or
                not 1<=shape[2]<=1024 or not 1<=shape[3]<=1024 or
                not callable(getattr(value,'is_floating_point',None)) or
                not value.is_floating_point()):
            raise ValueError('MODEL_OUTPUT_INVALID')
        return value
    def infer(self,rgb):
        from PIL import Image
        value=np.asarray(rgb,np.float32)
        if value.ndim!=3 or value.shape[2]!=3 or not np.isfinite(value).all() or np.any(value<0) or np.any(value>1):raise ValueError('MODEL_INPUT_INVALID')
        inputs=self.processor(images=Image.fromarray(np.rint(value*255).astype(np.uint8),'RGB'),return_tensors='pt');inputs={k:v.to(self.device) for k,v in inputs.items()}
        with self.torch.inference_mode():logits=self.model(**inputs)
        logits=self._bounded_logits(getattr(logits,'logits',None))
        return logits.detach().to('cpu',dtype=self.torch.float32).numpy()[0]

class ONNXU2Net:
    def __init__(self,path,backend):
        if backend!='cpu':raise ModelError('BACKEND_UNAVAILABLE')
        try:
            import onnxruntime as ort
            self.session=ort.InferenceSession(str(path),providers=['CPUExecutionProvider']);self.inputs=self.session.get_inputs();self.outputs=self.session.get_outputs()
            if len(self.inputs)!=1 or not self.outputs:raise ValueError()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    def infer(self,normalized_rgb):
        value=np.asarray(normalized_rgb,np.float32)
        if value.shape!=(320,320,3) or not np.isfinite(value).all():raise ValueError('MODEL_INPUT_INVALID')
        result=self.session.run([self.outputs[0].name],{self.inputs[0].name:np.ascontiguousarray(value.transpose(2,0,1)[None])})[0]
        result=np.asarray(result,np.float32)
        while result.ndim>2 and result.shape[0]==1:result=result[0]
        if result.shape==(1,320,320):result=result[0]
        if result.shape!=(320,320) or not np.isfinite(result).all():raise ValueError('MODEL_OUTPUT_INVALID')
        return result

class ONNXIdentity512:
    """Qualified 112px sRGB face embedding with no backend fallback."""
    def __init__(self,path,backend):
        if backend not in ('cpu','cuda'):raise ModelError('BACKEND_UNAVAILABLE')
        try:
            import onnxruntime as ort
            provider='CUDAExecutionProvider' if backend=='cuda' else 'CPUExecutionProvider'
            if provider not in ort.get_available_providers():raise ModelError('BACKEND_UNAVAILABLE')
            self.session=ort.InferenceSession(str(path),providers=[provider])
            if self.session.get_providers()!=[provider]:raise ModelError('BACKEND_UNAVAILABLE')
            inputs=self.session.get_inputs();outputs=self.session.get_outputs()
            if (len(inputs)!=1 or len(outputs)!=1 or
                    inputs[0].shape!=[1,3,112,112] or
                    outputs[0].shape!=[1,512] or
                    inputs[0].type!='tensor(float)' or
                    outputs[0].type!='tensor(float)'):
                raise ModelError('MODEL_ARCHITECTURE_INVALID')
            self.input_name=inputs[0].name;self.output_name=outputs[0].name
        except ModelError:raise
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc

    def infer(self,aligned_srgb):
        value=np.asarray(aligned_srgb,np.float32)
        if (value.shape!=(112,112,3) or not np.isfinite(value).all() or
                np.any(value<0) or np.any(value>1)):
            raise ValueError('MODEL_INPUT_INVALID')
        nchw=np.ascontiguousarray(value.transpose(2,0,1)[None])
        result=np.asarray(self.session.run([self.output_name],
                          {self.input_name:nchw})[0],np.float32)
        if result.shape!=(1,512) or not np.isfinite(result).all():
            raise ValueError('MODEL_OUTPUT_INVALID')
        norm=float(np.linalg.norm(result[0].astype(np.float64)))
        if not np.isfinite(norm) or norm<=1e-8:
            raise ValueError('MODEL_OUTPUT_INVALID')
        normalized=result[0].astype(np.float64)/norm
        return normalized.astype(np.float32)

class TorchScriptPrompt:
    def __init__(self,path,backend):
        import torch
        try:self.torch=torch;self.device=_device(backend);self.model=torch.jit.load(str(path),map_location=self.device).eval()
        except Exception as exc:raise ModelError('MODEL_ARCHITECTURE_INVALID') from exc
    def infer(self,rgb,points,labels,box):
        tensors=[self.torch.from_numpy(np.ascontiguousarray(x)).to(self.device) for x in (rgb.transpose(2,0,1)[None],points[None],labels[None],box[None])]
        with self.torch.inference_mode():result=self.model(*tensors)
        if not isinstance(result,(tuple,list)) or len(result)!=2:raise ValueError('MODEL_OUTPUT_INVALID')
        return tuple(x.detach().to('cpu',dtype=self.torch.float32).numpy() for x in result)

def factories():
    def torchscript(expected):
        def create(entry,files,backend):
            if entry.architecture!=expected:raise ModelError('MODEL_ARCHITECTURE_INVALID')
            return TorchScriptRGB(_single_weight(files),backend)
        return create
    def diffusion(entry,files,backend):
        if entry.architecture!=DIFFUSERS_ARCHITECTURE:raise ModelError('MODEL_ARCHITECTURE_INVALID')
        root=Path(os.path.commonpath([str(path) for path in files.values()]))
        if root.is_file():root=root.parent
        if not (root/'model_index.json').is_file():raise ModelError('MODEL_LAYOUT_INVALID')
        return DiffusersInpaint(root,backend)
    def lraspp(entry,files,backend):
        if entry.architecture!=LRASPP_ARCHITECTURE or entry.weight_id!=LRASPP_WEIGHT_ID or entry.preprocessing_id!='person_guided_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        return TorchvisionLRASPP(_single_weight(files),backend)
    def keypoints(entry,files,backend):
        if entry.architecture!=KEYPOINT_ARCHITECTURE or entry.weight_id!=KEYPOINT_WEIGHT_ID or entry.preprocessing_id!='keypointrcnn_coco_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        return TorchvisionKeypointRCNN(_single_weight(files),backend)
    def sfd(entry,files,backend):
        if entry.architecture!=SFD_ARCHITECTURE or entry.weight_id!=SFD_WEIGHT_ID or entry.preprocessing_id!='sfd_long1024_bgr_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        return TorchScriptSFD(_single_weight(files),backend)
    def fan(entry,files,backend):
        if entry.architecture!=FAN_ARCHITECTURE or entry.weight_id!=FAN_WEIGHT_ID or entry.preprocessing_id!='fan2d_crop256_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        return TorchScriptFAN(_single_weight(files),backend)
    def transformer(expected,weight_id,preprocessing_id,kind):
        def create(entry,files,backend):
            if entry.architecture!=expected or entry.weight_id!=weight_id or entry.preprocessing_id!=preprocessing_id:raise ModelError('MODEL_ARCHITECTURE_INVALID')
            root=Path(os.path.commonpath([str(path) for path in files.values()]));root=root if root.is_dir() else root.parent
            if not (root/'config.json').is_file():raise ModelError('MODEL_LAYOUT_INVALID')
            return TransformersVision(root,backend,kind)
        return create
    def segformer(entry,files,backend):
        if entry.architecture!=SEGFORMER_ARCHITECTURE or entry.weight_id!=SEGFORMER_WEIGHT_ID or entry.preprocessing_id!='segformer_ade512_official_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        root=Path(os.path.commonpath([str(path) for path in files.values()]));root=root if root.is_dir() else root.parent
        return TransformersSegformer(root,backend)
    def u2net(entry,files,backend):
        if entry.architecture!=U2NET_ARCHITECTURE or entry.weight_id!=U2NET_WEIGHT_ID or entry.preprocessing_id!='u2net_320_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        candidates=[path for name,path in files.items() if Path(name).suffix.lower()=='.onnx']
        if len(candidates)!=1:raise ModelError('MODEL_LAYOUT_INVALID')
        return ONNXU2Net(candidates[0],backend)
    def faceparse(entry,files,backend):
        if entry.architecture!=FACE_PARSE_ARCHITECTURE or entry.weight_id!=FACE_PARSE_WEIGHT_ID or entry.preprocessing_id!='bisenet_face19_crop512_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        return TorchScriptRGB(_single_weight(files),backend)
    def identity512(entry,files,backend):
        if (entry.architecture!=IDENTITY512_ARCHITECTURE or
                entry.weight_id!=IDENTITY512_WEIGHT_ID or
                entry.preprocessing_id!='srgb_nchw_float32_0_1'):
            raise ModelError('MODEL_ARCHITECTURE_INVALID')
        candidates=[path for name,path in files.items() if Path(name).suffix.lower()=='.onnx']
        if len(candidates)!=1 or len(files)!=1:raise ModelError('MODEL_LAYOUT_INVALID')
        return ONNXIdentity512(candidates[0],backend)
    def reflection_rgb(entry,files,backend):
        if (entry.architecture != 'onnx.reflection_rgb.v2_1'
                or entry.preprocessing_id != 'srgb_nchw_float32_0_1'
                or backend not in ('cpu', 'cuda')):
            raise ModelError('MODEL_ARCHITECTURE_INVALID')
        candidates = [path for name,path in files.items()
                      if Path(name).suffix.lower() == '.onnx']
        if len(candidates) != 1 or len(files) != 1:
            raise ModelError('MODEL_LAYOUT_INVALID')
        from .reflection_onnx import ONNXReflection
        return ONNXReflection(candidates[0],backend)
    def mobilesam(entry,files,backend):
        if entry.architecture!=MOBILESAM_ARCHITECTURE or entry.weight_id!=MOBILESAM_WEIGHT_ID or entry.preprocessing_id!='mobilesam_long1024_v1':raise ModelError('MODEL_ARCHITECTURE_INVALID')
        return TorchScriptPrompt(_single_weight(files),backend)
    def capture(family):
        def create(entry,files,backend):
            from .capture_onnx import ONNXCapture
            if entry.architecture!='onnx.'+family+'.v2_1' or entry.preprocessing_id!='srgb_nchw_float32_0_1':
                raise ModelError('MODEL_ARCHITECTURE_INVALID')
            candidates=[path for name,path in files.items() if Path(name).suffix.lower()=='.onnx']
            if len(candidates)!=1 or len(files)!=1:raise ModelError('MODEL_LAYOUT_INVALID')
            return ONNXCapture(candidates[0],backend,family)
        return create
    return {
        'lraspp_mobilenet_v3_large':lraspp,
        'keypointrcnn_resnet50_fpn':keypoints,
        'sfd':sfd,
        'fan2d':fan,
        'identity512':identity512,
        'reflection_rgb':reflection_rgb,
        'eye_open':capture('eye_open'),
        'glare_mask':capture('glare_mask'),
        'dinov2_vits14':transformer(DINO_ARCHITECTURE,DINO_WEIGHT_ID,'dinov2_pad518_v1','dino'),
        'depth_anything_v2_small':transformer(DEPTH_ARCHITECTURE,DEPTH_WEIGHT_ID,'depth_anything_v2_small_518_v1','depth'),
        'segformer_b0_ade':segformer,
        'u2net':u2net,
        'bisenet_face19':faceparse,
        'mobilesam':mobilesam,
        'scunet_color_real_psnr':torchscript('torchscript.scunet_color_real_psnr.v1'),
        'realesrgan_x2':torchscript('torchscript.realesrgan_x2.v1'),
        'realesrgan_x4':torchscript('torchscript.realesrgan_x4.v1'),
        'big_lama':torchscript('torchscript.big_lama.v1'),'sd15_inpaint':diffusion,
    }
