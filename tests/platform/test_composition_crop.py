import hashlib
from uuid import uuid4
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.composition import CompositionParams,ImageStructure,LineSegment,NormalizedRect,ProtectedPoint,StructureParams
from photo_worker.contracts.images import Canvas,ColorSpace,MaskRef,TransformChain,WorkingImage
from photo_worker.operations.composition.plan import choose_crop,choose_outpaint,estimate_rotation,execute
from photo_worker.operations.composition.structure import execute as execute_structure
from photo_worker.runtime.scratch import ScratchSpace
from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
from types import SimpleNamespace
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext
from test_asset_adapters import context_for


def ref(value=None):
    return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest() if value is not None else 'a'*64)


class Artifacts:
    def __init__(self,image,structure):
        self.image=image;self.structure=structure;self.image_ref=ref(image);self.structure.image=self.image_ref;self.structure_ref=ref(structure);self.value=CompositionParams(structure=self.structure_ref,aspect_width=4,aspect_height=5);self.staged=[]
    def params(self,model):return self.value
    def contract(self,r,model):return self.structure if r==self.structure_ref else self.image
    def stage_contract(self,value,role):self.staged.append((role,value));return ref(value)


def image(w=120,h=100):
    source='b'*64;canvas=Canvas(width=w,height=h,orientation=1,space_id='source',source_sha256=source)
    return WorkingImage(pixels=ref(),canvas=canvas,color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24'),dtype='float32',alpha=None,transform_chain=TransformChain.create([]))


def test_rotation_uses_robust_axis_deviation():
    lines=[LineSegment(x0=0,y0=0,x1=1,y1=.035,confidence=1),LineSegment(x0=.2,y0=0,x1=.2,y1=1,confidence=1)]
    assert -1.1<estimate_rotation(lines,5)<-.9


def test_crop_preserves_protected_point_and_aspect():
    chosen=choose_crop(120,100,.8,0,[(60,10)],[NormalizedRect(x0=.3,y0=.1,x1=.7,y1=.95)],.12)
    assert chosen is not None
    _,(x,y,w,h),_,retained,_,_=chosen
    assert w/h==.8 and x<=60<x+w and y<=10<y+h and retained>.8


def test_outpaint_geometry_is_bounded_and_subject_aware():
    side,pixels=choose_outpaint(100,100,1.5,[NormalizedRect(x0=.1,y0=.1,x1=.4,y1=.9)],[])
    assert (side,pixels)==('right',50)
    side,pixels=choose_outpaint(100,100,.5,[NormalizedRect(x0=.1,y0=.6,x1=.9,y1=.9)],[])
    assert (side,pixels)==('top',100)


def test_plan_is_bound_to_image_and_flags_large_rotation():
    structure=ImageStructure(image=ref(),lines=[LineSegment(x0=0,y0=0,x1=1,y1=.09)],protected_points=[ProtectedPoint(x=.5,y=.1,kind='head')],subjects=[NormalizedRect(x0=.3,y0=.05,x1=.7,y1=.95)])
    artifacts=Artifacts(image(),structure);ctx=context_for(artifacts.structure_ref,'composition.plan');result=execute(ctx,artifacts);plan=artifacts.staged[0][1]
    assert plan.image==artifacts.image_ref and plan.structure==artifacts.structure_ref
    assert abs(plan.output_width/plan.output_height-.8)<.011 and plan.protected_points_preserved
    assert plan.disposition=='review' and 'large_rotation' in plan.reasons and result.review_required
    matrix=np.asarray(plan.forward_matrix).reshape(3,3);assert abs(np.linalg.det(matrix))>.9


class StructureArtifacts:
    def __init__(self,root):
        self.paths={};self.staged=[];canvas=Canvas(width=80,height=64,orientation=1,space_id='source',source_sha256='b'*64);chain=TransformChain.create([])
        def saved(name,value):
            path=root/f'{name}.npy';np.save(path,value,allow_pickle=False);r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths[r.artifact_id]=path;return r
        rgb=np.zeros((64,80,3),np.float32);rgb[20:22,:, :]=1;pixels=saved('rgb',rgb);mask=np.zeros((64,80),np.float32);mask[8:58,25:55]=1;mask_ref=saved('mask',mask)
        self.image=WorkingImage(pixels=pixels,canvas=canvas,color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24'),dtype='float32',alpha=None,transform_chain=chain);self.image_ref=ref(self.image);self.subject=MaskRef(artifact=mask_ref,canvas=canvas,transform_chain=chain,selector='person',confidence=.9,model_lock_id='d'*64,protected=False);self.value=StructureParams(image=self.image_ref,subject=self.subject,max_lines=32);self.inputs=[self.image_ref,pixels,mask_ref]
    def params(self,model):return self.value
    def contract(self,r,model):return self.image
    @contextmanager
    def input(self,r,*,suffix):yield SimpleNamespace(path=self.paths[r.artifact_id])
    def stage_contract(self,value,role):self.staged.append((role,value));return ref(value)


def test_structure_extracts_lines_and_protects_subject(tmp_path):
    a=StructureArtifacts(tmp_path);now=datetime.now(timezone.utc);ctx=ExecutionContext(Lease(SimpleNamespace(inputs=a.inputs),uuid4(),1,now+timedelta(minutes=1),now+timedelta(minutes=1),{'backend':'cpu','resource_estimate':{'ram_bytes':536870912}},'e'*64));scratch=tmp_path/'scratch';scratch.mkdir()
    with ScratchSpace(scratch,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as space:a.scratch=space;result=execute_structure(ctx,a)
    structure=a.staged[0][1];assert structure.image==a.image_ref and structure.lines and len(structure.subjects)==1 and structure.protected_points[0].kind=='person' and not result.review_required
