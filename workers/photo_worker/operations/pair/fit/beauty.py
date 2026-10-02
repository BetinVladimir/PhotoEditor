from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.beauty_fit import fit_face
from photo_worker.algorithms.beauty_roi import plan_roi
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.pair_effects import BeautyFaceFitEvidence,BeautyFit,BeautyFitParams
from photo_worker.contracts.style import Beauty,Disabled
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def match_landmarks(before,after,matrix,before_canvas,after_canvas):
    candidates=[]
    for i,bf in enumerate(before):
        center=np.mean([[p.x*(before_canvas.width-1),p.y*(before_canvas.height-1)] for p in bf.points],axis=0)
        q=matrix@np.array([center[0],center[1],1.]);q=q[:2]/q[2];q/=np.array([max(1,after_canvas.width-1),max(1,after_canvas.height-1)])
        for j,af in enumerate(after):
            ac=np.mean([[p.x,p.y] for p in af.points],axis=0);d=float(np.linalg.norm(q-ac))
            if d<=.2:candidates.append((d,bf.object_id.hex,af.object_id.hex,i,j))
    used_b=set();used_a=set();matches=[]
    for _,_,_,i,j in sorted(candidates):
        if i not in used_b and j not in used_a:used_b.add(i);used_a.add(j);matches.append((before[i],after[j]))
    return matches


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(BeautyFitParams);before=artifacts.contract(p.before,WorkingImage);after=artifacts.contract(p.after,WorkingImage);alignment=artifacts.contract(p.alignment,PairAlignment)
        bfs=artifacts.contract(p.before_landmarks,FaceLandmarks);afs=artifacts.contract(p.after_landmarks,FaceLandmarks)
        if before.color!=after.color or before.dtype!='float32' or after.dtype!='float32' or max(before.canvas.width*before.canvas.height,after.canvas.width*after.canvas.height)>40000000:raise ValueError('Beauty fit image invalid')
        if alignment.before!=p.before or alignment.after!=p.after or alignment.before_canvas!=before.canvas or alignment.after_canvas!=after.canvas or bfs.image!=p.before or afs.image!=p.after:raise ValueError('Beauty fit binding mismatch')
        if p.skin.canvas!=after.canvas or p.skin.transform_chain!=after.transform_chain or p.permanent is not None and (p.permanent.canvas!=after.canvas or p.permanent.transform_chain!=after.transform_chain):raise ValueError('Beauty fit geometry mismatch')
        matrix=np.asarray(alignment.before_to_after,np.float64).reshape(3,3);bindings=match_landmarks(bfs.landmarks,afs.landmarks,matrix,before.canvas,after.canvas)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=after.canvas.height,after.canvas.width;evidence=[];accepted=[]
    with ExitStack() as stack:
        def mapped(ref,shape):return stack.enter_context(read_array(stack.enter_context(artifacts.input(ref,suffix='.npy')).path,shape,'float32',ctx.check_active,unit_interval=True))
        src=mapped(before.pixels,(before.canvas.height,before.canvas.width,3));dst=mapped(after.pixels,(h,w,3));src_aligned=cv2.warpPerspective(np.asarray(src),matrix,(w,h),flags=cv2.INTER_LINEAR,borderMode=cv2.BORDER_REFLECT_101)
        skin=mapped(p.skin.artifact,(h,w));protected=np.zeros((h,w),np.float32) if p.permanent is None else mapped(p.permanent.artifact,(h,w))
        for bf,af in bindings:
            confidence=min(min(x.confidence for x in bf.points),min(x.confidence for x in af.points),p.skin.confidence);reason='applied'
            try:roi=plan_roi(af);r=roi.rect
            except ValueError:reason='invalid_roi';roi=None
            if confidence<p.min_confidence:reason='low_confidence'
            elif roi is not None and roi.face_width<64:reason='face_too_small'
            if roi is None or reason!='applied':baseline=fitted=0.;improvement=0.;ratio=None;ok=False
            else:
                sl=(slice(r.y,r.y+r.height),slice(r.x,r.x+r.width));baseline,best=fit_face(np.asarray(src_aligned[sl]),np.asarray(dst[sl]),np.asarray(skin[sl],np.float32),np.asarray(protected[sl],np.float32),roi.face_width,confidence,ctx.check_active);fitted,values,ratio=best;improvement=(baseline-fitted)/max(baseline,1e-12);ok=improvement>=.02
                if not ok:reason='no_improvement'
                else:accepted.append(values)
            evidence.append(BeautyFaceFitEvidence(before_object_id=bf.object_id,after_object_id=af.object_id,baseline_loss=baseline,fitted_loss=fitted,improvement=max(-1,min(1,improvement)),texture_energy_ratio=ratio,confidence=confidence,accepted=ok,reason=reason))
    warnings=[]
    if len(accepted)<len(bindings):warnings.append('faces_skipped')
    if not accepted:warnings.append('no_measured_improvement');beauty=Disabled(enabled=False);confidence=0.
    else:
        values=np.asarray(accepted);spread=float(np.max(np.ptp(values,axis=0))) if len(values)>1 else 0.
        if spread>.35:warnings.append('inconsistent_faces');beauty=Disabled(enabled=False);confidence=0.
        else:
            median=np.median(values,axis=0);beauty=Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=float(median[0]),tone_evenness=float(median[1]),texture_keep=float(median[2]),skin_mask='face68_conservative_v1',protect_features=True);confidence=float(np.median([x.improvement for x in evidence if x.accepted]))
    result=BeautyFit(params=p,beauty=beauty,confidence=max(0,min(1,confidence)),faces=evidence,search_digest=canonical_hash({'grid':[0,.1,.25,.5,.75,1],'texture':[.75,.9,1],'refine':[3,.05]}),warnings=warnings);ref=artifacts.stage_contract(result,'beauty_fit')
    return OperationResult(outputs=[ref],review_required=True,bytes_written=len(canonical_bytes(result)))


def beauty_fit_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'beauty_fit'})) as a:return execute(ctx,a)
    return [Operation('pair.fit.beauty','cpu',run)]
