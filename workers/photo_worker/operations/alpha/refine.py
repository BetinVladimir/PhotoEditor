from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.algorithms.mask_png import png16_bound,write_png16
from photo_worker.contracts.alpha import MattingParams,MattingReport
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _required_refine_ram(height:int,width:int)->int:
    """Reserve full-frame trimap, boundary, result and PNG buffers."""
    if type(height) is not int or type(width) is not int or height<1 or width<1:raise ValueError('MATTING_SIZE_INVALID')
    return 512*1024**2+height*width*160


def _boundary_roi(boundary,radius):
    if (boundary.dtype!=np.bool_ or boundary.ndim!=2 or boundary.size==0
            or type(radius) is not int or radius<0):raise ValueError('MATTING_BOUNDARY_INVALID')
    rows=np.any(boundary,axis=1);cols=np.any(boundary,axis=0)
    if not np.any(rows):return None
    h,w=boundary.shape
    x0=max(0,int(np.argmax(cols))-radius);x1=min(w,w-int(np.argmax(cols[::-1]))+radius)
    y0=max(0,int(np.argmax(rows))-radius);y1=min(h,h-int(np.argmax(rows[::-1]))+radius)
    return x0,y0,x1,y1


def build_trimap(alpha,low=.1,high=.9,radius=2):
    foreground=alpha>=high;background=alpha<=low;uncertain=~(foreground|background)
    kernel=cv2.getStructuringElement(cv2.MORPH_ELLIPSE,(2*radius+1,2*radius+1));soft=cv2.dilate(uncertain.astype(np.uint8),kernel)>0
    edge=(cv2.dilate(foreground.astype(np.uint8),kernel)>0)&(cv2.dilate(background.astype(np.uint8),kernel)>0);boundary=soft|edge
    trimap=np.where(foreground,1.,0.).astype(np.float64);trimap[boundary]=.5;return trimap,boundary


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(MattingParams);image=artifacts.contract(p.image,WorkingImage)
        if (p.alpha.canvas!=image.canvas or p.alpha.transform_chain!=image.transform_chain
                or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
                or image.color.encoding!='gamma24' or image.canvas.width*image.canvas.height>200000000):raise ValueError('MATTING_IMAGE_INVALID')
        if ram<_required_refine_ram(image.canvas.height,image.canvas.width):raise OperationFailure('RESOURCE_OOM')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width;radius=max(2,round(p.band_fraction*max(h,w)))
    with ExitStack() as stack:
        rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));alpha=stack.enter_context(read_array(stack.enter_context(artifacts.input(p.alpha.artifact,suffix='.npy')).path,(h,w),'float32',ctx.check_active,unit_interval=True));trimap,boundary=build_trimap(np.asarray(alpha),p.trimap_low,p.trimap_high,radius);bounds=_boundary_roi(boundary,radius);reason='no_boundary';result=np.asarray(alpha).copy();roi=[0,0,1,1]
        if bounds is not None:
            x0,y0,x1,y1=bounds;roi=[x0,y0,x1-x0,y1-y0]
            if x1-x0>p.max_roi or y1-y0>p.max_roi:raise OperationFailure('RESOURCE_OOM',False)
            try:
                from pymatting import estimate_alpha_cf
                srgb,_=work_to_model(np.asarray(rgb[y0:y1,x0:x1]));refined=np.asarray(estimate_alpha_cf(srgb.astype(np.float64),trimap[y0:y1,x0:x1]),np.float32)
            except Exception as exc:raise OperationFailure('MATTING_FAILED') from exc
            if refined.shape!=(y1-y0,x1-x0) or not np.isfinite(refined).all():raise OperationFailure('MATTING_FAILED')
            local=boundary[y0:y1,x0:x1];part=result[y0:y1,x0:x1];part[local]=np.clip(refined[local],0,1);reason='refined'
        bound=png16_bound(w,h)
        with artifacts.scratch.reserve(h*w*4+8192+bound) as reservation:
            path=reservation.path('refined-alpha','.npy');mm=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w))
            try:mm[:]=result;mm.flush();ctx.check_active()
            finally:mm._mmap.close()
            png=reservation.path('refined-alpha','.png')
            with artifacts.scratch.open_writer(png,reservation) as stream:write_png16(result,stream,max_bytes=bound,check_active=ctx.check_active)
            reservation.validate();ctx.check_active();aref=artifacts.stage(path,reservation,'alpha_mask','application/octet-stream');ctx.check_active();pref=artifacts.stage(png,reservation,'alpha_mask_png','image/png');written=path.stat().st_size+png.stat().st_size
    confidence=float(1-np.mean((result>.1)&(result<.9)));mask=MaskRef(artifact=aref,canvas=image.canvas,transform_chain=image.transform_chain,object_id=p.alpha.object_id,selector='alpha',confidence=confidence,model_lock_id=None,protected=False);ctx.check_active();mref=artifacts.stage_contract(mask,'alpha_mask_ref');report=MattingReport(image=p.image,input_alpha=p.alpha.artifact,output_alpha=aref,uncertain_pixels=int(np.count_nonzero(boundary)),roi=roi,band_radius=radius,confidence=confidence,reason=reason);ctx.check_active();rref=artifacts.stage_contract(report,'matting_report')
    return OperationResult(outputs=[aref,pref,mref,rref],review_required=confidence<.75,bytes_written=written+len(canonical_bytes(mask))+len(canonical_bytes(report)))


def refine_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'alpha_mask','alpha_mask_png','alpha_mask_ref','matting_report'})) as a:return execute(ctx,a)
    return [Operation('alpha.refine','cpu',run)]
