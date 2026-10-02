from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
from photo_worker.contracts.pose import PoseParams,PoseSet
from photo_worker.operations.pose.detect import execute
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext,OperationFailure

LOCK='a'*64


class Engine:
    def __init__(self):self.input=None
    def infer(self,value):
        self.input=value.copy()
        boxes=np.array([[12,4,23,19],[1,2,10,18]],np.float32)
        scores=np.array([.8,.95],np.float32)
        points=np.zeros((2,17,3),np.float32)
        points[0,:,0]=18;points[0,:,1]=10
        points[1,:,0]=5;points[1,:,1]=8
        return {'boxes':boxes,'scores':scores,'keypoints':points,'keypoint_scores':np.full((2,17),.9,np.float32)}


class Pool:
    def __init__(self):self.lock=SimpleNamespace(lock_id=LOCK);self.engine=Engine()
    @contextmanager
    def acquire(self,model_id,backend):
        assert model_id=='keypointrcnn_resnet50_fpn' and backend=='mps';yield self.engine


class Artifacts:
    def __init__(self,root,lock=LOCK,alpha=True):
        rgb=np.full((20,24,3),.4,np.float32);path=root/'rgb.npy';np.save(path,rgb,allow_pickle=False)
        pixel=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths={pixel.artifact_id:path}
        canvas=Canvas(width=24,height=20,orientation=1,space_id='source',source_sha256='b'*64)
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24');alpha_ref=None
        if alpha:
            values=np.full((20,24),65535,np.uint16);values[:,:2]=0;apath=root/'alpha.npy';np.save(apath,values,allow_pickle=False)
            alpha_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(apath.read_bytes()).hexdigest());self.paths[alpha_ref.artifact_id]=apath
        self.image=WorkingImage(pixels=pixel,canvas=canvas,color=color,dtype='float32',alpha=alpha_ref,transform_chain=TransformChain.create([]))
        self.image_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(self.image)).hexdigest())
        self.raw=PoseParams(image=self.image_ref,model_lock_id=lock).model_dump(mode='json');self.staged=[]
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.image
    @contextmanager
    def input(self,ref,*,suffix):yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self,value,role):
        self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def context(a):
    now=datetime.now(timezone.utc);inputs=[a.image_ref,a.image.pixels]+([a.image.alpha] if a.image.alpha else [])
    lease=Lease(command=SimpleNamespace(inputs=inputs),worker_id=uuid4(),fence=1,until=now+timedelta(minutes=5),deadline=now+timedelta(minutes=5),execution_profile={'backend':'mps','resource_estimate':{'ram_bytes':2147483648}},input_digest='d'*64)
    return ExecutionContext(lease)


def test_pose_adapter_binds_sorted_people_to_source_geometry(tmp_path):
    a=Artifacts(tmp_path);pool=Pool();result=execute(context(a),a,pool,'mps')
    assert pool.engine.input.shape==(20,24,3) and np.all(pool.engine.input[:,:2]==0)
    role,value=a.staged[0];assert role=='pose_set';PoseSet.model_validate_json(canonical_bytes(value))
    assert value.detected_count==2 and value.accepted_count==2 and value.alpha_excluded_fraction==pytest.approx(2/24)
    assert value.poses[0].points[0].x==pytest.approx(5/24) and value.poses[0].points[0].y==pytest.approx(8/20)
    assert value.poses[0].canvas==a.image.canvas and value.poses[0].transform_chain==a.image.transform_chain
    assert len(result.outputs)==1 and not result.review_required


def test_pose_adapter_rejects_lock_and_bad_model_shape(tmp_path):
    a=Artifacts(tmp_path,lock='f'*64);pool=Pool()
    with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'mps')
    assert error.value.error.code=='MODEL_LOCK_MISMATCH'
    a=Artifacts(tmp_path,alpha=False);pool=Pool();pool.engine.infer=lambda value:{'boxes':np.zeros((1,4),np.float32),'scores':np.ones(1,np.float32),'keypoints':np.zeros((1,16,3),np.float32),'keypoint_scores':np.ones((1,16),np.float32)}
    with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'mps')
    assert error.value.error.code=='MODEL_OUTPUT_INVALID' and not a.staged
