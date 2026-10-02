from contextlib import ExitStack
import cv2
import numpy as np

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.composition import ImageStructure,LineSegment,NormalizedRect,ProtectedPoint,StructureParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<536870912:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(StructureParams);image=artifacts.contract(p.image,WorkingImage);h,w=image.canvas.height,image.canvas.width
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or h*w>200000000:raise ValueError()
        if p.subject is not None and (p.subject.canvas!=image.canvas or p.subject.transform_chain!=image.transform_chain or p.subject.protected):raise ValueError()
        required=[image.pixels,*([image.alpha] if image.alpha else []),*([p.subject.artifact] if p.subject else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError()
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    with ExitStack() as stack:
        rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True))
        scale=min(1.,1024/max(h,w));pw,ph=max(1,round(w*scale)),max(1,round(h*scale))
        preview=cv2.resize(np.asarray(rgb),(pw,ph),interpolation=cv2.INTER_AREA) if scale<1 else np.asarray(rgb)
        gray=np.rint(np.clip(preview.mean(axis=2),0,1)*255).astype(np.uint8);edges=cv2.Canny(gray,p.canny_low,p.canny_high)
        found=cv2.HoughLinesP(edges,1,np.pi/180,threshold=max(20,min(pw,ph)//20),minLineLength=max(10,min(pw,ph)//12),maxLineGap=max(3,min(pw,ph)//100))
        raw=[] if found is None else [tuple(int(v) for v in line[0]) for line in found]
        raw.sort(key=lambda q:(-((q[2]-q[0])**2+(q[3]-q[1])**2),q))
        lines=[LineSegment(x0=x0/max(1,pw-1),y0=y0/max(1,ph-1),x1=x1/max(1,pw-1),y1=y1/max(1,ph-1),confidence=1) for x0,y0,x1,y1 in raw[:p.max_lines]]
        subjects=[];protected=[]
        if p.subject is not None:
            mask=stack.enter_context(read_array(stack.enter_context(artifacts.input(p.subject.artifact,suffix='.npy')).path,(h,w),'float32',ctx.check_active,unit_interval=True));ys,xs=np.nonzero(np.asarray(mask)>=.5)
            if len(xs):
                x0,x1,y0,y1=int(xs.min()),int(xs.max()),int(ys.min()),int(ys.max());subjects=[NormalizedRect(x0=x0/w,y0=y0/h,x1=(x1+1)/w,y1=(y1+1)/h)];protected=[ProtectedPoint(x=(x0+x1+1)/(2*w),y=y0/h,kind='person',confidence=p.subject.confidence)]
    result=ImageStructure(image=p.image,lines=lines,protected_points=protected,subjects=subjects);ref=artifacts.stage_contract(result,'image_structure')
    return OperationResult(outputs=[ref],review_required=not lines or p.subject is not None and not subjects,bytes_written=len(canonical_bytes(result)))


def structure_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'image_structure'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('composition.structure','cpu',run)]
