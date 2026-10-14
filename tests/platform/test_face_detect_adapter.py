from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.face import FaceBoxes,FaceDetectParams
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
from photo_worker.operations.face.detect import SFD_MEAN,execute
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext,OperationFailure

LOCK='a'*64


class Engine:
    def __init__(self):self.input=None
    def infer(self,value):
        self.input=value.copy()
        return np.array([[2,2,12,16,.95],[3,3,12,16,.9],[14,4,23,18,.85],[1,1,4,4,.2]],np.float32)


class Pool:
    def __init__(self):self.lock=SimpleNamespace(lock_id=LOCK);self.engine=Engine()
    @contextmanager
    def acquire(self,model_id,backend):
        assert model_id=='sfd' and backend=='cpu';yield self.engine


class Artifacts:
    def __init__(self,root,lock=LOCK):
        self.rgb=np.full((20,24,3),.4,np.float32)
        path=root/'rgb.npy';np.save(path,self.rgb,allow_pickle=False)
        pixel=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths={pixel.artifact_id:path}
        alpha=np.full((20,24),65535,np.uint16);alpha[:,:2]=0;apath=root/'alpha.npy';np.save(apath,alpha,allow_pickle=False)
        alpha_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(apath.read_bytes()).hexdigest());self.paths[alpha_ref.artifact_id]=apath
        canvas=Canvas(width=24,height=20,orientation=1,space_id='source',source_sha256='b'*64)
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24')
        self.image=WorkingImage(pixels=pixel,canvas=canvas,color=color,dtype='float32',alpha=alpha_ref,transform_chain=TransformChain.create([]))
        self.image_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(self.image)).hexdigest())
        self.raw=FaceDetectParams(image=self.image_ref,model_lock_id=lock).model_dump(mode='json');self.staged=[]
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.image
    @contextmanager
    def input(self,ref,*,suffix):yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self,value,role):
        self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def context(a):
    now=datetime.now(timezone.utc);inputs=[a.image_ref,a.image.pixels,a.image.alpha]
    lease=Lease(command=SimpleNamespace(inputs=inputs),worker_id=uuid4(),fence=1,until=now+timedelta(minutes=5),deadline=now+timedelta(minutes=5),execution_profile={'backend':'cpu','resource_estimate':{'ram_bytes':1073741824}},input_digest='d'*64)
    def require_declared_input(ref):
        if ref not in lease.command.inputs:raise ValueError('Undeclared fixture input')
    a.require_declared_input=require_declared_input
    return ExecutionContext(lease)


def test_face_detect_preprocesses_nms_and_binds_boxes(tmp_path):
    a=Artifacts(tmp_path);pool=Pool();result=execute(context(a),a,pool,'cpu')
    srgb=work_to_model(a.rgb)[0];srgb[:,:2]=0
    assert np.allclose(pool.engine.input,srgb[...,::-1]*255-SFD_MEAN)
    role,value=a.staged[0];assert role=='face_boxes';FaceBoxes.model_validate_json(canonical_bytes(value))
    assert value.candidate_count==3 and value.accepted_count==2 and value.alpha_excluded_fraction==pytest.approx(2/24)
    assert value.faces[0].confidence==pytest.approx(.95) and value.faces[0].bbox_normalized.x0==pytest.approx(2/24)
    assert value.faces[1].confidence==pytest.approx(.85) and len({f.object_id for f in value.faces})==2
    assert len(result.outputs)==1 and not result.review_required


def test_face_detect_rejects_lock_and_out_of_canvas_output(tmp_path):
    a=Artifacts(tmp_path,lock='f'*64);pool=Pool()
    with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'cpu')
    assert error.value.error.code=='MODEL_LOCK_MISMATCH'
    a=Artifacts(tmp_path);pool=Pool();pool.engine.infer=lambda value:np.array([[-1,0,4,4,.9]],np.float32)
    with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'cpu')
    assert error.value.error.code=='MODEL_OUTPUT_INVALID' and not a.staged
