from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.face import FaceBoxes
from photo_worker.contracts.face_parse import FaceLabels19,FaceParseParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult

MEAN=np.array([.485,.456,.406],np.float32);STD=np.array([.229,.224,.225],np.float32)


def crop_geometry(box,w,h,size=512,expansion=1.35):
    x0,x1=box.x0*w,box.x1*w;y0,y1=box.y0*h,box.y1*h;side=max(x1-x0,y1-y0)*expansion;cx=(x0+x1)/2;cy=(y0+y1)/2;left,top=cx-side/2,cy-side/2
    forward=np.array([[size/side,0,-left*size/side],[0,size/side,-top*size/side]],np.float32);return forward,(left,top,side)


def decode_logits(raw,size=512):
    value=np.asarray(raw,np.float32)
    if value.ndim==4 and value.shape[0]==1:value=value[0]
    if value.shape==(size,size,19):value=value.transpose(2,0,1)
    if value.shape!=(19,size,size) or not np.isfinite(value).all():raise ValueError('MODEL_OUTPUT_INVALID')
    maximum=np.max(value,axis=0);exp=np.exp(value-maximum);prob=exp/np.sum(exp,axis=0);return np.argmax(prob,axis=0).astype(np.uint16),np.max(prob,axis=0).astype(np.float32)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(FaceParseParams);image=artifacts.contract(p.image,WorkingImage);faces=artifacts.contract(p.faces,FaceBoxes)
        if p.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        if faces.image!=p.image or image.dtype!='float32' or image.canvas.width*image.canvas.height>200000000:raise ValueError('Face parse binding invalid')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width;labels=np.zeros((h,w),np.uint16);confidence=np.zeros((h,w),np.float32);overlap=False;scores=[]
    try:
        with ExitStack() as stack:
            rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));srgb,clipped=work_to_model(rgb)
            with pool.acquire(p.model_id,backend) as engine:
                for face in sorted(faces.faces,key=lambda x:x.object_id.hex):
                    matrix,geometry=crop_geometry(face.bbox_normalized,w,h,p.crop_size,p.expansion);crop=cv2.warpAffine(srgb,matrix,(p.crop_size,p.crop_size),flags=cv2.INTER_LINEAR,borderMode=cv2.BORDER_REFLECT_101);raw=engine.infer((crop-MEAN)/STD);local,prob=decode_logits(raw,p.crop_size);left,top,side=geometry;inverse=np.array([[side/p.crop_size,0,left],[0,side/p.crop_size,top]],np.float32);restored=cv2.warpAffine(local,inverse,(w,h),flags=cv2.INTER_NEAREST,borderMode=cv2.BORDER_CONSTANT,borderValue=0);restored_prob=cv2.warpAffine(prob,inverse,(w,h),flags=cv2.INTER_LINEAR,borderMode=cv2.BORDER_CONSTANT,borderValue=0);active=restored>0;overlap|=bool(np.any(active&(labels>0)));replace=active&(restored_prob>confidence);labels[replace]=restored[replace];confidence[replace]=restored_prob[replace];scores.append(float(np.mean(prob)))
        with artifacts.scratch.reserve(h*w*2+8192) as reservation:
            path=reservation.path('face-labels19','.npy');mm=np.lib.format.open_memmap(path,mode='w+',dtype='<u2',shape=(h,w));mm[:]=labels;mm.flush();mm._mmap.close();reservation.validate();lref=artifacts.stage(path,reservation,'face_labels','application/octet-stream');written=path.stat().st_size
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    mean=float(np.mean(scores)) if scores else 0.;warnings=[]
    if not faces.faces:warnings.append('no_faces')
    if faces.faces and mean<.75:warnings.append('low_confidence')
    if overlap:warnings.append('overlapping_faces')
    if np.any(clipped):warnings.append('source_srgb_clipped')
    result=FaceLabels19(image=p.image,faces=p.faces,labels=lref,canvas=image.canvas,transform_chain=image.transform_chain,model_id=p.model_id,model_lock_id=p.model_lock_id,backend=backend,preprocessing_id=p.preprocessing_id,parsed_faces=len(faces.faces),mean_confidence=mean,warnings=warnings);ref=artifacts.stage_contract(result,'face_labels_ref');return OperationResult(outputs=[lref,ref],review_required=bool(warnings),bytes_written=written+len(canonical_bytes(result)))


def face_parse_operations(config:ArtifactConfig,pool:ModelPool,backend:str):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'face_labels','face_labels_ref'})) as a:return execute(ctx,a,pool,backend)
    return [Operation('face.parse','gpu',run)]
