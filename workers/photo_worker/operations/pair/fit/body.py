"""Pair body-geometry fitting with explicit association and conservative skip gates."""
from __future__ import annotations
from contextlib import ExitStack
import numpy as np

from photo_worker.algorithms.body_fit import bounded_coefficients,match_poses,pose_measurements
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.body_fit import BodyCoefficients,BodyFit,BodyFitMetrics,BodyFitParams,BodyObjectFit
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.pose import PoseSet
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _zero():return BodyCoefficients(face_slim=0,waist_slim=0,hip_slim=0,torso_slim=0,leg_length=0)


def _face_width(faces,pose):
    if faces is None or not faces.landmarks:return None
    nose=np.array([pose.points[0].x,pose.points[0].y]);best=None
    for face in faces.landmarks:
        pts=np.array([[p.x,p.y,p.confidence] for p in face.points],np.float64)
        if min(pts[0,2],pts[16,2])<.5:continue
        center=np.mean(pts[:,:2],axis=0);distance=float(np.linalg.norm(center-nose));width=float(np.linalg.norm(pts[0,:2]-pts[16,:2]))
        if width>0 and (best is None or distance<best[0]):best=(distance,width)
    return None if best is None or best[0]>.35 else best[1]


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<536870912:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(BodyFitParams);before=artifacts.contract(params.before,WorkingImage);after=artifacts.contract(params.after,WorkingImage)
        alignment=artifacts.contract(params.alignment,PairAlignment);bp=artifacts.contract(params.before_pose,PoseSet);ap=artifacts.contract(params.after_pose,PoseSet)
        bf=artifacts.contract(params.before_faces,FaceLandmarks) if params.before_faces else None;af=artifacts.contract(params.after_faces,FaceLandmarks) if params.after_faces else None
        if alignment.crop_aware or alignment.before!=params.before or alignment.after!=params.after or alignment.before_canvas!=before.canvas or alignment.after_canvas!=after.canvas:raise ValueError('BODY_ALIGNMENT_INVALID')
        if bp.image!=params.before or ap.image!=params.after or bf is not None and bf.image!=params.before or af is not None and af.image!=params.after:raise ValueError('BODY_FEATURE_BINDING_INVALID')
        if params.before_person.canvas!=before.canvas or params.before_person.transform_chain!=before.transform_chain or params.after_person.canvas!=after.canvas or params.after_person.transform_chain!=after.transform_chain:raise ValueError('BODY_MASK_BINDING_INVALID')
        required=[params.before,params.after,params.alignment,params.before_pose,params.after_pose,params.before_person.artifact,params.after_person.artifact]
        if params.before_faces:required.extend([params.before_faces,params.after_faces])
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('BODY_INPUT_UNDECLARED')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,4)
    try:
        with ExitStack() as stack:
            def mask(ref,shape):
                local=stack.enter_context(artifacts.input(ref,suffix='.npy'));return stack.enter_context(read_array(local.path,shape,'float32',ctx.check_active,unit_interval=True))
            before_mask=mask(params.before_person.artifact,(before.canvas.height,before.canvas.width));after_mask=mask(params.after_person.artifact,(after.canvas.height,after.canvas.width))
            matches=match_poses(bp.poses,ap.poses,alignment.before_to_after,before.canvas,after.canvas);ctx.progress(1,4)
            matrix=np.asarray(alignment.before_to_after,np.float64).reshape(3,3);linear=matrix[:2,:2]
            width_scale=float(np.linalg.norm(linear@np.array([before.canvas.width,0]))/after.canvas.width)
            length_scale=float(np.linalg.norm(linear@np.array([0,before.canvas.height]))/after.canvas.height)
            objects=[]
            for match in matches:
                ctx.check_active();source=bp.poses[match.before];target=ap.poses[match.after]
                bm=pose_measurements(source,before_mask);am=pose_measurements(target,after_mask)
                reason='applied';accepted=True
                if match.margin<.1:reason='ambiguous';accepted=False
                elif bm is None or am is None:reason='missing_anchors';accepted=False
                requested=_zero();silhouette_error=1.
                if bm is not None and am is not None:
                    values=bounded_coefficients(bm,am,width_scale,length_scale)
                    before_scaled=bm.copy();before_scaled[:3]*=width_scale;before_scaled[3]*=length_scale
                    silhouette_error=float(min(1,np.mean(np.abs(before_scaled-am)/np.maximum(before_scaled,1e-6))))
                    face=0.;bw=_face_width(bf,source);aw=_face_width(af,target)
                    if bw is not None and aw is not None:face=min(.05,max(0,(bw*width_scale-aw)/(2*bw*width_scale)))
                    requested=BodyCoefficients(face_slim=face,torso_slim=float(values[0]),waist_slim=float(values[1]),hip_slim=float(values[2]),leg_length=float(values[3]))
                pose_conf=min(np.mean([p.confidence for p in source.points]),np.mean([p.confidence for p in target.points]))
                confidence=float(np.clip(min(pose_conf,params.before_person.confidence,params.after_person.confidence,1-match.cost),0,1))
                if accepted and confidence<params.min_confidence:reason='low_confidence';accepted=False
                background=float(alignment.median_reprojection or 0)
                metrics=BodyFitMetrics(iou=match.iou,landmark_error=match.landmark_error,silhouette_error=silhouette_error,background_anchor_error=min(1,background))
                objects.append(BodyObjectFit(before_object_id=source.object_id,after_object_id=target.object_id,confidence=confidence,match_cost=match.cost,ambiguity_margin=match.margin,requested=requested,applied=requested if accepted else _zero(),metrics=metrics,accepted=accepted,reason=reason))
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    warnings=[]
    if not bp.poses or not ap.poses:warnings.append('no_people')
    if len(matches)<max(len(bp.poses),len(ap.poses)):warnings.append('unmatched_people')
    if any(x.reason=='ambiguous' for x in objects):warnings.append('ambiguous_match')
    if any(x.reason=='low_confidence' for x in objects):warnings.append('low_confidence')
    if bf is None:warnings.append('face_evidence_unavailable')
    ctx.progress(2,4)
    result=BodyFit(params=params,objects=objects,warnings=warnings);ref=artifacts.stage_contract(result,'body_fit');ctx.progress(4,4)
    return OperationResult(outputs=[ref],review_required=bool(warnings) or any(not x.accepted for x in objects),bytes_written=len(canonical_bytes(result)))


def body_fit_operations(config:ArtifactConfig)->list[Operation]:
    def fitted(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'body_fit'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('pair.fit.body','cpu',fitted)]
