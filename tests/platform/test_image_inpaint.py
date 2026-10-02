from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import Canvas,ColorSpace,MaskRef,TransformChain,WorkingImage
from photo_worker.contracts.restoration import InpaintParams,InpaintReport
from photo_worker.operations.image.inpaint import execute
from photo_worker.operations.image.generate import execute as execute_generation
from photo_worker.operations.image.outpaint import execute as execute_outpaint
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext,OperationFailure
from photo_worker.runtime.scratch import ScratchSpace

LOCK="a"*64
class Engine:
    def infer(self,rgb,mask):return np.where(mask[...,None]>0,.8,rgb).astype(np.float32)
    def generate(self,rgb,mask,**kwargs):
        assert kwargs==dict(prompt='fill',negative_prompt='',seed=7,steps=25,guidance_scale=7.5,scheduler='ddim_v1')
        return np.where(mask[...,None]>0,.75,rgb).astype(np.float32)
class Pool:
    def __init__(self,lock=LOCK,model='big_lama'):self.lock=SimpleNamespace(lock_id=lock);self.engine=Engine();self.model=model
    @contextmanager
    def acquire(self,model,backend):assert model==self.model and backend=="cuda";yield self.engine

class Artifacts:
    def __init__(self,root,lock=LOCK):
        self.paths={};self.manifests={};self.staged=[];self.uploads=[]
        canvas=Canvas(width=24,height=20,orientation=1,space_id="source",source_sha256="b"*64);chain=TransformChain.create([])
        def array(name,value):
            path=root/f"{name}.npy";np.save(path,value,allow_pickle=False);ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths[ref.artifact_id]=path;return ref
        source=np.full((20,24,3),.4,np.float32);self.source=source;pixels=array("source",source)
        color=ColorSpace(profile_id="work_rec2020_gamma24_v1",icc_sha256="c"*64,encoding="gamma24")
        image=WorkingImage(pixels=pixels,canvas=canvas,color=color,dtype="float32",alpha=None,transform_chain=chain)
        self.image_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(image)).hexdigest());self.manifests[self.image_ref.artifact_id]=image
        target=np.zeros((20,24),np.float32);target[5:15,6:18]=1;protected=np.zeros_like(target);protected[8:12,10:14]=1
        self.target=target>=.5;self.protected=protected>=.5
        target_ref=array("target",target);protected_ref=array("protected",protected)
        mask=MaskRef(artifact=target_ref,canvas=canvas,transform_chain=chain,selector="manual",confidence=1,model_lock_id=None,protected=False)
        guard=MaskRef(artifact=protected_ref,canvas=canvas,transform_chain=chain,selector="manual",confidence=1,model_lock_id=None,protected=True)
        self.raw=InpaintParams(image=self.image_ref,mask=mask,protected=[guard],confirmed=True,model_lock_id=lock,context_pixels=32,feather_pixels=2).model_dump(mode="json")
        self.inputs=[self.image_ref,pixels,target_ref,protected_ref]
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self,ref,*,suffix):yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self,path,reservation,role,media_type):
        reservation.validate_file(path);self.output=np.load(path,allow_pickle=False);ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.uploads.append(ref);return ref
    def stage_contract(self,value,role):self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())

def context(a):
    now=datetime.now(timezone.utc);command=SimpleNamespace(inputs=a.inputs)
    return ExecutionContext(Lease(command,uuid4(),1,now+timedelta(minutes=5),now+timedelta(minutes=5),{"backend":"cuda","resource_estimate":{"ram_bytes":2147483648}},"e"*64))

def test_inpaint_changes_only_confirmed_unprotected_region_and_always_reviews(tmp_path):
    a=Artifacts(tmp_path);ctx=context(a);root=tmp_path/"scratch";root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute(ctx,a,Pool(),"cuda")
    allowed=a.target & ~a.protected
    assert np.array_equal(a.output[~allowed],a.source[~allowed])
    assert np.array_equal(a.output[a.protected],a.source[a.protected]) and np.any(a.output[allowed]!=a.source[allowed])
    report=a.staged[1][1];assert result.review_required and report.candidate_requires_review and report.protected_pixels==16
    InpaintReport.model_validate_json(canonical_bytes(report))

def test_inpaint_lock_mismatch_fails_before_inputs(tmp_path):
    a=Artifacts(tmp_path,lock="f"*64)
    with pytest.raises(OperationFailure) as error:execute(context(a),a,Pool(),"cuda")
    assert error.value.error.code=="MODEL_LOCK_MISMATCH" and not a.uploads

def test_generation_is_seeded_and_restores_outside_and_protected(tmp_path):
    a=Artifacts(tmp_path);payload=a.raw;a.raw={"image":payload["image"],"mask":payload["mask"],"protected":payload["protected"],
        "confirmed":True,"prompt":"fill","negative_prompt":"","seed":7,"steps":25,"guidance_scale":7.5,
        "model_id":"sd15_inpaint","model_lock_id":LOCK,"feather_pixels":2}
    ctx=context(a);root=tmp_path/"scratch";root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute_generation(ctx,a,Pool(model='sd15_inpaint'),"cuda")
    allowed=a.target&~a.protected
    assert np.array_equal(a.output[~allowed],a.source[~allowed]) and np.array_equal(a.output[a.protected],a.source[a.protected])
    assert np.any(a.output[allowed]!=a.source[allowed]) and result.review_required
    report=a.staged[1][1];assert report.seed==7 and report.scheduler=='ddim_v1' and report.side is None

def test_outpaint_extends_one_side_and_preserves_source_exactly(tmp_path):
    a=Artifacts(tmp_path);a.raw={"image":a.image_ref.model_dump(mode="json"),"side":"left","pixels":8,"prompt":"extend",
        "negative_prompt":"","seed":9,"steps":25,"guidance_scale":7.5,"model_id":"sd15_inpaint","model_lock_id":LOCK,"feather_pixels":4}
    a.paths={key:value for key,value in a.paths.items() if value.name=='source.npy'};a.inputs=a.inputs[:2]
    def generated(rgb,mask,**kwargs):return np.where(mask[...,None]>0,.7,rgb).astype(np.float32)
    pool=Pool(model='sd15_inpaint');pool.engine.generate=generated;ctx=context(a);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute_outpaint(ctx,a,pool,'cuda')
    assert a.output.shape==(20,32,3) and np.array_equal(a.output[:,8:],a.source)
    image=a.staged[0][1];report=a.staged[1][1]
    assert image.canvas.width==32 and image.transform_chain.steps[-1].forward_matrix[2]==8
    assert report.side=='left' and report.source_rect==[8,0,24,20] and result.review_required
