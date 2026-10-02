from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.face import FaceBoxes,FaceLandmarks,LandmarkParams
from photo_worker.contracts.images import Canvas,ColorSpace,FaceBox,NormalizedRect,TransformChain,WorkingImage
from photo_worker.operations.face.landmarks import execute
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext,OperationFailure

LOCK='a'*64


class Engine:
    def __init__(self):self.inputs=[]
    def infer(self,value):
        self.inputs.append(value.copy());points=np.zeros((68,3),np.float32);points[:,0]=127.5;points[:,1]=127.5;points[:,2]=.9;return points


class Pool:
    def __init__(self):self.lock=SimpleNamespace(lock_id=LOCK);self.engine=Engine()
    @contextmanager
    def acquire(self,model_id,backend):
        assert model_id=='fan2d' and backend=='cuda';yield self.engine


class Artifacts:
    def __init__(self,root,lock=LOCK):
        rgb=np.full((100,200,3),.4,np.float32);path=root/'rgb.npy';np.save(path,rgb,allow_pickle=False)
        pixel=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths={pixel.artifact_id:path}
        canvas=Canvas(width=200,height=100,orientation=1,space_id='source',source_sha256='b'*64);chain=TransformChain.create([])
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24')
        self.image=WorkingImage(pixels=pixel,canvas=canvas,color=color,dtype='float32',alpha=None,transform_chain=chain)
        self.image_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(self.image)).hexdigest())
        self.face_id=uuid4();face=FaceBox(object_id=self.face_id,bbox_normalized=NormalizedRect(x0=.25,y0=.2,x1=.55,y1=.8),confidence=.95)
        self.faces_ref=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
        self.faces=FaceBoxes(image=self.image_ref,faces=[face],model_id='sfd',model_lock_id='e'*64,backend='cpu',preprocessing_id='sfd_long1024_bgr_v1',preview_width=200,preview_height=100,candidate_count=1,accepted_count=1,alpha_excluded_fraction=0,warnings=[])
        self.raw=LandmarkParams(image=self.image_ref,faces=self.faces_ref,model_lock_id=lock).model_dump(mode='json');self.staged=[]
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.image if ref==self.image_ref else self.faces
    @contextmanager
    def input(self,ref,*,suffix):yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self,value,role):self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def context(a):
    now=datetime.now(timezone.utc);inputs=[a.image_ref,a.image.pixels,a.faces_ref]
    lease=Lease(command=SimpleNamespace(inputs=inputs),worker_id=uuid4(),fence=1,until=now+timedelta(minutes=5),deadline=now+timedelta(minutes=5),execution_profile={'backend':'cuda','resource_estimate':{'ram_bytes':1073741824}},input_digest='d'*64)
    return ExecutionContext(lease)


def test_landmarks_crop_maps_points_and_preserves_face_identity(tmp_path):
    a=Artifacts(tmp_path);pool=Pool();result=execute(context(a),a,pool,'cuda')
    assert len(pool.engine.inputs)==1 and pool.engine.inputs[0].shape==(256,256,3)
    role,value=a.staged[0];assert role=='face_landmarks';FaceLandmarks.model_validate_json(canonical_bytes(value))
    face=value.landmarks[0];assert face.object_id==a.face_id and len(face.points)==68
    assert face.points[0].x==pytest.approx(.4,abs=.005) and face.points[0].y==pytest.approx(.5,abs=.005)
    assert face.canvas==a.image.canvas and face.transform_chain==a.image.transform_chain
    assert value.mean_confidence==pytest.approx(.9) and not result.review_required


def test_landmarks_rejects_wrong_binding_and_output_shape(tmp_path):
    a=Artifacts(tmp_path);a.faces.image=ArtifactRef(artifact_id=uuid4(),sha256='f'*64);pool=Pool()
    with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'cuda')
    assert error.value.error.code=='INPUT_INVALID'
    a=Artifacts(tmp_path);pool=Pool();pool.engine.infer=lambda value:np.zeros((67,3),np.float32)
    with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'cuda')
    assert error.value.error.code=='MODEL_OUTPUT_INVALID' and not a.staged
