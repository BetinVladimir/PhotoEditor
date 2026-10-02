from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.pair_effects import DefectEvidence,DefectFitParams
from photo_worker.contracts.style import Defects,Disabled
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def measure(source,target,eligible,check_active=lambda:None):
    # Evidence only: compact high-frequency residuals may be defects, but never
    # prove that an object should be removed or authorize a repair.
    check_active();sl=cv2.cvtColor(source,cv2.COLOR_RGB2GRAY);tl=cv2.cvtColor(target,cv2.COLOR_RGB2GRAY)
    sh=np.abs(sl-cv2.GaussianBlur(sl,(0,0),1.2));th=np.abs(tl-cv2.GaussianBlur(tl,(0,0),1.2));delta=sh-th
    values=delta[eligible]
    if values.size<64:return 0,0,0.
    median=float(np.median(values));mad=float(np.median(np.abs(values-median)));threshold=max(.01,median+4*1.4826*mad)
    binary=((delta>threshold)&eligible).astype(np.uint8);count,labels,stats,_=cv2.connectedComponentsWithStats(binary,8)
    reductions=[]
    for i in range(1,count):
        check_active();area=int(stats[i,cv2.CC_STAT_AREA])
        if not 1<=area<=256:continue
        chosen=labels==i;before=float(np.mean(sh[chosen]));after=float(np.mean(th[chosen]));reductions.append((before-after)/max(before,1e-6))
    return len(reductions),sum(x>.25 for x in reductions),float(np.median(reductions)) if reductions else 0.


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<536870912:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(DefectFitParams);before=artifacts.contract(p.before,WorkingImage);after=artifacts.contract(p.after,WorkingImage);alignment=artifacts.contract(p.alignment,PairAlignment)
        if alignment.before!=p.before or alignment.after!=p.after or alignment.before_canvas!=before.canvas or alignment.after_canvas!=after.canvas or before.color!=after.color or before.dtype!='float32' or after.dtype!='float32' or max(before.canvas.width*before.canvas.height,after.canvas.width*after.canvas.height)>40000000:raise ValueError('Defect fit requires bound alignment')
        if any(m.canvas!=after.canvas or m.transform_chain!=after.transform_chain for m in p.skin_masks) or p.permanent is not None and (p.permanent.canvas!=after.canvas or p.permanent.transform_chain!=after.transform_chain):raise ValueError('Defect fit mask geometry mismatch')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=after.canvas.height,after.canvas.width
    with ExitStack() as stack:
        def mapped(ref,shape):return stack.enter_context(read_array(stack.enter_context(artifacts.input(ref,suffix='.npy')).path,shape,'float32',ctx.check_active,unit_interval=True))
        src=mapped(before.pixels,(before.canvas.height,before.canvas.width,3));dst=mapped(after.pixels,(h,w,3));matrix=np.asarray(alignment.before_to_after,np.float64).reshape(3,3);src_aligned=cv2.warpPerspective(np.asarray(src),matrix,(w,h),flags=cv2.INTER_LINEAR,borderMode=cv2.BORDER_REFLECT_101);eligible=np.zeros((h,w),bool)
        for mask in p.skin_masks:eligible|=mapped(mask.artifact,(h,w))>=.75
        if p.permanent is not None:eligible&=mapped(p.permanent.artifact,(h,w))<.5
        components,improved,reduction=measure(src_aligned,np.asarray(dst),eligible,ctx.check_active)
    confidence=min(m.confidence for m in p.skin_masks);warnings=['review_policy_forced']
    if confidence<p.min_confidence:warnings.insert(0,'low_mask_confidence')
    if improved==0:warnings.insert(0,'no_residual_evidence')
    # Pair residuals can suggest default strength, while policy remains review.
    if improved and confidence>=p.min_confidence:
        strength=float(np.clip(max(0,reduction),0,1));module=Defects(enabled=True,algorithm='defectrepair_telea_v1',policy='review',strength=strength,diameter_face_fraction=[.002,.012],preserve_permanent_marks=True)
    else:module=Disabled(enabled=False)
    result=DefectEvidence(params=p,defects=module,confidence=confidence,residual_components=components,improved_components=improved,median_residual_reduction=max(-1,min(1,reduction)),evidence_digest=canonical_hash({'components':components,'improved':improved,'reduction':reduction,'policy':'review'}),warnings=warnings);ref=artifacts.stage_contract(result,'defect_evidence')
    return OperationResult(outputs=[ref],review_required=True,bytes_written=len(canonical_bytes(result)))


def defect_fit_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'defect_evidence'})) as a:return execute(ctx,a)
    return [Operation('pair.fit.defects','cpu',run)]
