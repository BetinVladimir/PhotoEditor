import hashlib,json
from uuid import uuid4
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.body import BodyParams,BodyWarpReport
from photo_worker.contracts.body_fit import BodyCoefficients,BodyFit,BodyFitMetrics,BodyFitParams,BodyObjectFit
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.images import Canvas,ColorSpace,Landmark,MaskRef,Pose17,TransformChain,WorkingImage
from photo_worker.contracts.pose import PoseSet
from photo_worker.operations.geometry.body import execute
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for
from test_image_color_adapter import Artifacts as BaseArtifacts


class Artifacts(BaseArtifacts):
    def __init__(self,root,strength=.5):
        self.paths={};self.contracts={};self.staged=[];self.uploads=[];self.outputs={}
        h=w=64;rgb=np.random.default_rng(9).random((h,w,3),dtype=np.float32);pixels=self.array(root,'rgb',rgb);alpha=self.array(root,'alpha',np.full((h,w),50000,np.uint16))
        canvas=Canvas(width=w,height=h,orientation=1,space_id='source',source_sha256=pixels.sha256);chain=TransformChain.create([])
        self.image=WorkingImage(pixels=pixels,canvas=canvas,color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='a'*64,encoding='gamma24'),dtype='float32',alpha=alpha,transform_chain=chain);self.image_ref=self.add(self.image);self.rgb=rgb
        values=[(.5,.12),(.46,.14),(.54,.14),(.43,.18),(.57,.18),(.4,.3),(.6,.3),(.36,.48),(.64,.48),(.34,.62),(.66,.62),(.42,.58),(.58,.58),(.42,.75),(.58,.75),(.42,.94),(.58,.94)]
        pose=Pose17(object_id=uuid4(),points=[Landmark(x=x,y=y,confidence=.95) for x,y in values],canvas=canvas,transform_chain=chain)
        poses=PoseSet(image=self.image_ref,poses=[pose],model_id='keypointrcnn_resnet50_fpn',model_lock_id='c'*64,backend='cpu',preprocessing_id='keypointrcnn_coco_v1',detected_count=1,accepted_count=1,alpha_excluded_fraction=0,warnings=[]);poses_ref=self.add(poses)
        person=np.zeros((h,w),np.float32);person[7:62,20:44]=1;person_ref=self.array(root,'person',person);mask=MaskRef(artifact=person_ref,canvas=canvas,transform_chain=chain,selector='person',confidence=.95,model_lock_id='d'*64,protected=False)
        coeff=BodyCoefficients(face_slim=0,waist_slim=.01,hip_slim=.01,torso_slim=.01,leg_length=0);after_ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
        alignment_ref=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
        fit_params=BodyFitParams(pair_id=uuid4(),before=self.image_ref,after=after_ref,alignment=alignment_ref,before_pose=poses_ref,after_pose=poses_ref,before_person=mask,after_person=mask)
        metrics=BodyFitMetrics(iou=.95,landmark_error=.01,silhouette_error=.01,background_anchor_error=.01)
        fit=BodyFit(params=fit_params,objects=[BodyObjectFit(before_object_id=pose.object_id,after_object_id=pose.object_id,confidence=.95,match_cost=.01,ambiguity_margin=.9,requested=coeff,applied=coeff,metrics=metrics,accepted=True,reason='applied')],warnings=[])
        fit_ref=self.add(fit)
        params=BodyParams(image=self.image_ref,poses=poses_ref,person=mask,fit=fit_ref,strength=strength)
        self.raw=params.model_dump(mode='json')
    def stage(self,path,reservation,role,media_type):
        reservation.validate_file(path);self.outputs[role]=np.load(path,allow_pickle=False);data=path.read_bytes();ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(data).hexdigest());self.uploads.append(ref);return ref
    def stage_contract(self,value,role):self.staged.append((role,value));return self.add(value)


def context(a):
    ctx=context_for(a.image_ref,'geometry.body');p=BodyParams.model_validate_json(json.dumps(a.raw));ctx.lease.command.inputs.extend([a.image.pixels,a.image.alpha,p.poses,p.person.artifact]);
    if p.fit is not None:ctx.lease.command.inputs.append(p.fit)
    ctx.lease.execution_profile.update(resource_estimate={'ram_bytes':1073741824});return ctx


def test_geometry_body_publishes_one_validated_dense_warp(tmp_path):
    a=Artifacts(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute(context(a),a)
    image=a.staged[0][1];report=a.staged[1][1];BodyWarpReport.model_validate_json(canonical_bytes(report))
    assert report.applied and report.metrics.min_jacobian>=.5 and report.metrics.max_displacement_pixels<=1.28
    assert len(image.transform_chain.steps)==1 and image.transform_chain.steps[0].inverse_field==a.uploads[0]
    assert not np.array_equal(a.outputs['working_rgb'],a.rgb) and len(result.outputs)==5


def test_zero_strength_is_exact_pixel_identity(tmp_path):
    a=Artifacts(tmp_path,0);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute(context(a),a)
    assert np.array_equal(a.outputs['working_rgb'],a.rgb)
    assert a.staged[1][1].reason=='zero_strength' and not a.staged[1][1].applied and not result.review_required


def test_transferable_style_coefficients_apply_without_pair_fit(tmp_path):
    a=Artifacts(tmp_path);p=BodyParams.model_validate_json(json.dumps(a.raw));fit=a.contracts[p.fit.artifact_id]
    a.raw=BodyParams(image=p.image,poses=p.poses,person=p.person,coefficients=fit.objects[0].applied,strength=p.strength).model_dump(mode='json')
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute(context(a),a)
    assert a.staged[1][1].applied and len(result.outputs)==5
    assert not np.array_equal(a.outputs['working_rgb'],a.rgb)
