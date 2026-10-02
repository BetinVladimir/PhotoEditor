from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.body_fit import BodyFit,BodyFitParams
from photo_worker.contracts.images import Canvas,ColorSpace,Landmark,MaskRef,Pose17,TransformChain,WorkingImage
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.pose import PoseSet
from photo_worker.operations.pair.fit.body import execute
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext


def ref(char):return ArtifactRef(artifact_id=uuid4(),sha256=char*64)


def pose(canvas,chain,x_shift=0):
    values=[(.5+x_shift,.12),(.46+x_shift,.14),(.54+x_shift,.14),(.43+x_shift,.18),(.57+x_shift,.18),(.4+x_shift,.3),(.6+x_shift,.3),(.36+x_shift,.48),(.64+x_shift,.48),(.34+x_shift,.62),(.66+x_shift,.62),(.42+x_shift,.58),(.58+x_shift,.58),(.42+x_shift,.75),(.58+x_shift,.75),(.42+x_shift,.94),(.58+x_shift,.94)]
    return Pose17(object_id=uuid4(),points=[Landmark(x=x,y=y,confidence=.95) for x,y in values],canvas=canvas,transform_chain=chain)


class Artifacts:
    def __init__(self,root,duplicates=False):
        canvas=Canvas(width=100,height=100,orientation=1,space_id='source',source_sha256='a'*64);chain=TransformChain.create([]);color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='b'*64,encoding='gamma24')
        self.before,self.after=ref('1'),ref('2');self.before_image=WorkingImage(pixels=ref('3'),canvas=canvas,color=color,dtype='float32',alpha=None,transform_chain=chain);self.after_image=self.before_image.model_copy(update={'pixels':ref('4')})
        self.alignment_ref=ref('5');self.alignment=PairAlignment(before=self.before,after=self.after,before_canvas=canvas,after_canvas=canvas,before_to_after=[1,0,0,0,1,0,0,0,1],overlap=.95,median_reprojection=.1,inlier_count=20,confidence='measured',crop_aware=False,warnings=[])
        self.bp_ref,self.ap_ref=ref('6'),ref('7');before_poses=[pose(canvas,chain)];after_poses=[pose(canvas,chain)]
        if duplicates:before_poses.append(pose(canvas,chain));after_poses.append(pose(canvas,chain))
        self.bp=PoseSet(image=self.before,poses=before_poses,model_id='keypointrcnn_resnet50_fpn',model_lock_id='c'*64,backend='cpu',preprocessing_id='keypointrcnn_coco_v1',detected_count=len(before_poses),accepted_count=len(before_poses),alpha_excluded_fraction=0,warnings=[])
        self.ap=self.bp.model_copy(update={'image':self.after,'poses':after_poses})
        bm=np.zeros((100,100),np.float32);bm[:,30:70]=1;am=np.zeros((100,100),np.float32);am[:,35:65]=1
        bp=root/'before.npy';ap=root/'after.npy';np.save(bp,bm,allow_pickle=False);np.save(ap,am,allow_pickle=False)
        self.bm_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(bp.read_bytes()).hexdigest());self.am_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(ap.read_bytes()).hexdigest());self.paths={self.bm_ref.artifact_id:bp,self.am_ref.artifact_id:ap}
        before_mask=MaskRef(artifact=self.bm_ref,canvas=canvas,transform_chain=chain,selector='person',confidence=.95,model_lock_id='d'*64,protected=False);after_mask=before_mask.model_copy(update={'artifact':self.am_ref})
        self.params_value=BodyFitParams(pair_id=uuid4(),before=self.before,after=self.after,alignment=self.alignment_ref,before_pose=self.bp_ref,after_pose=self.ap_ref,before_person=before_mask,after_person=after_mask);self.staged=[]
        self.contracts={self.before.artifact_id:self.before_image,self.after.artifact_id:self.after_image,self.alignment_ref.artifact_id:self.alignment,self.bp_ref.artifact_id:self.bp,self.ap_ref.artifact_id:self.ap}
    def params(self,model):return model.model_validate_json(canonical_bytes(self.params_value))
    def contract(self,artifact,model):return self.contracts[artifact.artifact_id]
    @contextmanager
    def input(self,artifact,*,suffix):yield SimpleNamespace(path=self.paths[artifact.artifact_id])
    def stage_contract(self,value,role):self.staged.append((role,value));return ref('e')


def context(a):
    now=datetime.now(timezone.utc);p=a.params_value
    inputs=[p.before,p.after,p.alignment,p.before_pose,p.after_pose,p.before_person.artifact,p.after_person.artifact]
    lease=Lease(command=SimpleNamespace(inputs=inputs),worker_id=uuid4(),fence=1,until=now+timedelta(minutes=5),deadline=now+timedelta(minutes=5),execution_profile={'backend':'cpu','resource_estimate':{'ram_bytes':536870912}},input_digest='f'*64)
    return ExecutionContext(lease)


def test_body_fit_associates_and_bounds_silhouette_coefficients(tmp_path):
    a=Artifacts(tmp_path);result=execute(context(a),a)
    role,value=a.staged[0];assert role=='body_fit';BodyFit.model_validate_json(canonical_bytes(value))
    fit=value.objects[0];assert fit.accepted and fit.reason=='applied' and fit.metrics.iou==1
    assert fit.applied.waist_slim==.08 and fit.applied.hip_slim==.08 and fit.applied.torso_slim==.08 and fit.applied.face_slim==0
    assert value.warnings==['face_evidence_unavailable'] and result.review_required


def test_body_fit_ambiguous_people_are_identity(tmp_path):
    a=Artifacts(tmp_path,duplicates=True);execute(context(a),a);value=a.staged[0][1]
    assert all(not fit.accepted and fit.reason=='ambiguous' and not any(fit.applied.model_dump().values()) for fit in value.objects)
    assert 'ambiguous_match' in value.warnings
