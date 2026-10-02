from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import Canvas,ColorSpace,MaskRef,TransformChain,WorkingImage
from photo_worker.contracts.person import PersonParams,PersonReport
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.operations.mask.person import MEAN,STD,_preview,execute
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext,OperationFailure
from photo_worker.runtime.scratch import ScratchSpace

LOCK='a'*64

class Engine:
    def __init__(self):self.input=None
    def infer(self,value):
        self.input=value.copy();h,w=value.shape[:2]
        logits=np.full((21,h,w),-4,np.float32);logits[0]=0;logits[15,:,w//3:2*w//3]=5
        return logits

class Pool:
    def __init__(self):self.lock=SimpleNamespace(lock_id=LOCK);self.engine=Engine()
    @contextmanager
    def acquire(self,model_id,backend):
        assert model_id=='lraspp_mobilenet_v3_large' and backend=='cuda';yield self.engine

class Artifacts:
    def __init__(self,root,lock=LOCK,alpha=False):
        self.rgb=np.full((20,24,3),.4,np.float32);path=root/'rgb.npy';np.save(path,self.rgb,allow_pickle=False)
        self.pixel=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths={self.pixel.artifact_id:path}
        canvas=Canvas(width=24,height=20,orientation=1,space_id='source',source_sha256='b'*64)
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24');alpha_ref=None
        if alpha:
            values=np.full((20,24),65535,np.uint16);values[:,:2]=0;apath=root/'alpha.npy';np.save(apath,values,allow_pickle=False)
            alpha_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(apath.read_bytes()).hexdigest());self.paths[alpha_ref.artifact_id]=apath
        self.image=WorkingImage(pixels=self.pixel,canvas=canvas,color=color,dtype='float32',alpha=alpha_ref,transform_chain=TransformChain.create([]))
        self.image_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(self.image)).hexdigest())
        self.raw=PersonParams(image=self.image_ref,model_lock_id=lock,guided_tile=256).model_dump(mode='json');self.staged=[];self.arrays={};self.uploads=[]
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.image
    @contextmanager
    def input(self,ref,*,suffix):yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self,path,reservation,role,media_type):
        reservation.validate_file(path);ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.uploads.append((role,media_type,ref))
        if role=='person_mask':self.arrays[role]=np.load(path,allow_pickle=False)
        return ref
    def stage_contract(self,value,role):
        self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())

def context(a):
    now=datetime.now(timezone.utc);inputs=[a.image_ref,a.pixel]+([a.image.alpha] if a.image.alpha else [])
    lease=Lease(command=SimpleNamespace(inputs=inputs),worker_id=uuid4(),fence=1,until=now+timedelta(minutes=5),deadline=now+timedelta(minutes=5),execution_profile={'backend':'cuda','resource_estimate':{'ram_bytes':1073741824}},input_digest='d'*64)
    return ExecutionContext(lease)

def test_person_adapter_preprocesses_softmax_refines_and_publishes(tmp_path):
    a=Artifacts(tmp_path,alpha=True);pool=Pool();ctx=context(a);scratch=tmp_path/'scratch';scratch.mkdir()
    with ScratchSpace(scratch,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as space:a.scratch=space;result=execute(ctx,a,pool,'cuda')
    assert pool.engine.input.shape==(640,768,3)
    expected=(_preview(work_to_model(a.rgb)[0],768)-MEAN)/STD
    assert np.allclose(pool.engine.input,expected)
    mask=a.arrays['person_mask'];assert mask.shape==(20,24) and mask.dtype==np.float32 and np.all(mask[:,:2]==0) and mask[:,10:14].mean()>.5
    manifest=a.staged[0][1];report=a.staged[1][1]
    assert isinstance(manifest,MaskRef) and manifest.selector=='person' and manifest.model_lock_id==LOCK and not manifest.protected
    PersonReport.model_validate_json(canonical_bytes(report));assert report.person_class==15 and report.alpha_excluded_fraction>0
    assert [x[0] for x in a.uploads]==['person_mask','person_mask_png'] and len(result.outputs)==4

def test_person_adapter_rejects_lock_before_io(tmp_path):
    a=Artifacts(tmp_path,lock='f'*64);pool=Pool()
    with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'cuda')
    assert error.value.error.code=='MODEL_LOCK_MISMATCH' and not a.uploads

def test_person_output_shape_is_strict(tmp_path):
    a=Artifacts(tmp_path);pool=Pool();pool.engine.infer=lambda value:np.zeros((20,*value.shape[:2]),np.float32)
    scratch=tmp_path/'scratch';scratch.mkdir()
    with ScratchSpace(scratch,uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as space:
        a.scratch=space
        with pytest.raises(OperationFailure) as error:execute(context(a),a,pool,'cuda')
    assert error.value.error.code=='MODEL_OUTPUT_INVALID' and not a.uploads
