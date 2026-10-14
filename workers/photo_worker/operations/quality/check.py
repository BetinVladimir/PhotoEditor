"""Technical master/export acceptance gate; reference metrics never imply perceptual truth."""
from contextlib import ExitStack
import hashlib,math
import cv2
import numpy as np
from PIL import Image
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.quality import QualityParams,QualityReport,ReferenceMetrics,TechnicalMetrics
from photo_worker.runtime.arrays import read_array,read_alpha_array
from .working_metrics import measure_working_metrics
from .decision import _decision
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _technical(rgb,seams,finite=True,visible=None):
    below=float(np.mean(rgb<0));above=float(np.mean(rgb>1));safe=np.nan_to_num(rgb,nan=0,posinf=1,neginf=0);selected=safe if visible is None else safe[visible];black=float(np.mean(selected<=1/65535)) if selected.size else 0.;white=float(np.mean(selected>=1-1/65535)) if selected.size else 0.;gray=np.mean(np.clip(safe,0,1),axis=2).astype(np.float32);sharp=float(cv2.Laplacian(gray,cv2.CV_32F).var());values=[];h,w=gray.shape
    for seam in seams:
        if seam.orientation=='vertical':
            x=min(max(1,round(seam.position*(w-1))),w-1);values.extend(np.abs(safe[:,x]-safe[:,x-1]).reshape(-1).tolist())
        else:
            y=min(max(1,round(seam.position*(h-1))),h-1);values.extend(np.abs(safe[y]-safe[y-1]).reshape(-1).tolist())
    seam=float(np.percentile(values,99)) if values else 0.
    return TechnicalMetrics(below_zero_fraction=below,above_one_fraction=above,clipped_black_fraction=black,clipped_white_fraction=white,sharpness_laplacian=sharp,seam_p99_delta=seam,finite=finite)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:p=artifacts.params(QualityParams)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    reference=None;icc=True;orientation=True
    if p.target_kind=='working':
        try:
            image=artifacts.contract(p.target,WorkingImage);required=[p.target,image.pixels,*([image.alpha] if image.alpha else [])]
            baseline=None if p.baseline is None else artifacts.contract(p.baseline,WorkingImage)
            if baseline is not None:required.extend([p.baseline,baseline.pixels,*([baseline.alpha] if baseline.alpha else [])])
            required.extend(m.artifact for m in p.protected)
            if list(ctx.lease.command.inputs)!=required or image.dtype!='float32':raise ValueError('Quality binding mismatch')
            for mask in p.protected:
                if mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain or not mask.protected:raise ValueError('Protected geometry mismatch')
            if baseline is not None and (baseline.canvas!=image.canvas or baseline.color!=image.color or baseline.dtype!='float32'):raise ValueError('Baseline mismatch')
            with ExitStack() as stack:
                def array(ref,shape,unit=True):local=stack.enter_context(artifacts.input(ref,suffix='.npy'));return stack.enter_context(read_array(local.path,shape,'float32',ctx.check_active,unit_interval=unit))
                rgb=array(image.pixels,(image.canvas.height,image.canvas.width,3),unit=False);visible=None;alpha=None
                if image.alpha is not None:
                    local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'));alpha=stack.enter_context(read_alpha_array(local.path,(image.canvas.height,image.canvas.width),ctx.check_active))
                if baseline is None and not p.seams:
                    metrics=measure_working_metrics(rgb,alpha,check_active=ctx.check_active)
                else:
                    visible=None if alpha is None else np.asarray(alpha)>0
                    metrics=_technical(rgb,p.seams,visible=visible)
                if baseline is not None:
                    base=array(baseline.pixels,rgb.shape);delta=np.abs(np.asarray(rgb)-np.asarray(base));mse=float(np.mean(delta*delta));protected_max=None
                    if p.protected:
                        union=np.zeros(rgb.shape[:2],bool)
                        for mask in p.protected:union|=array(mask.artifact,rgb.shape[:2])>=.5
                        protected_max=float(np.max(delta[union])) if np.any(union) else 0.
                    reference=ReferenceMetrics(mean_absolute_error=float(np.mean(delta)),psnr_db=min(200.,float(-10*math.log10(max(mse,1e-20)))),protected_max_delta=protected_max)
        except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    else:
        try:
            encoded=artifacts.contract(p.target,EncodedImage)
            if list(ctx.lease.command.inputs)!=[p.target,encoded.artifact]:raise ValueError('Encoded quality binding')
            suffix={'jpeg':'.jpg','png':'.png','tiff':'.tiff'}[encoded.format]
            with artifacts.input(encoded.artifact,suffix=suffix) as local:
                with Image.open(local.path) as source:
                    orientation=source.getexif().get(274,1)==1;profile=source.info.get('icc_profile');icc=profile is not None and hashlib.sha256(profile).hexdigest()==encoded.icc_sha256;rgb=np.asarray(source.convert('RGB'),np.float32)/255
            metrics=_technical(rgb,p.seams)
        except (OSError,ValueError) as exc:raise OperationFailure('INPUT_INVALID') from exc
    status,reasons=_decision(metrics,reference,icc,orientation);report=QualityReport(target=p.target,target_kind=p.target_kind,stage=p.stage,threshold_profile_id=p.threshold_profile_id,status=status,technical=metrics,reference=reference,reasons=reasons);ref=artifacts.stage_contract(report,'quality_report');return OperationResult(outputs=[ref],review_required=status!='pass',bytes_written=len(canonical_bytes(report)))


def quality_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'quality_report'})) as a:return execute(ctx,a)
    return [Operation('quality.check','cpu',run)]
