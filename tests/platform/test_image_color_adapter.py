from contextlib import contextmanager
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.algorithms.color import apply_global,trilinear,ColorDiagnostics
from photo_worker.algorithms.fitting import identity_color
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.contracts.resolved import ResolveParams,ResolvedStyle,ResolvedModules,AbsoluteLUTRef,PrototypeWeight
from photo_worker.contracts.style import Disabled
from photo_worker.operations.image.color import execute
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_asset_adapters import context_for
from test_style_resolve import profile

class Artifacts:
    def __init__(self,root,adaptive):
        self.paths={};self.contracts={};self.staged=[];self.uploads=[]
        self.rgb=np.random.default_rng(3).random((39,601,3),dtype=np.float32)
        pixels=self.array(root,'pixels',self.rgb)
        alpha=self.array(root,'alpha',np.full((39,601),10000,np.uint16))
        self.image=WorkingImage(pixels=pixels,canvas=Canvas(width=601,height=39,orientation=1,space_id='oriented:test',source_sha256=pixels.sha256),
            color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='a'*64,encoding='gamma24'),dtype='float32',alpha=alpha,transform_chain=TransformChain.create([]))
        self.image_ref=self.add(self.image)
        style=profile();self.config=identity_color().model_copy(update={'gains_rgb':[2.,.8,1.4],'saturation':1.5})
        lut=None;self.grid=None
        if adaptive:
            self.grid=np.broadcast_to(np.array([.2,.4,.6],np.float32),(33,33,33,3)).copy()
            lut=AbsoluteLUTRef(artifact=self.array(root,'lut',self.grid),size=33)
        self.resolved=ResolvedStyle.create(params=ResolveParams(style=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),image=self.image_ref),
            mode='adaptive' if adaptive else 'fixed',fixed=None if adaptive else self.config,absolute_lut=lut,local=Disabled(),strength=0.,
            modules=ResolvedModules(beauty=style.beauty,defects=style.defects,geometry=style.geometry,extensions=style.extensions,models=style.models),
            neighbors=[PrototypeWeight(prototype_id='one',distance=0.,weight=1.)] if adaptive else [],
            distance=0. if adaptive else None,uncovered_fraction=0. if adaptive else None,reasons=[],needs_review=False)
        resolved_ref=self.add(self.resolved)
        self.raw=dict(image=self.image_ref.model_dump(mode='json'),resolved=resolved_ref.model_dump(mode='json'))
    def array(self,root,name,values):
        path=root/(name+'.npy');np.save(path,values)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths[ref.artifact_id]=path;return ref
    def add(self,value):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest());self.contracts[ref.artifact_id]=value;return ref
    def params(self,model):return model.model_validate_json(json.dumps(self.raw))
    def contract(self,ref,model):return self.contracts[ref.artifact_id]
    @contextmanager
    def input(self,ref,*,suffix):yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self,path,reservation,role,media_type):
        reservation.validate_file(path)
        assert role=='working_rgb' and media_type=='application/octet-stream'
        self.output=np.load(path,allow_pickle=False)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.uploads.append(ref);return ref
    def stage_contract(self,value,role):
        assert list(self.scratch.directory.iterdir())==[]
        self.staged.append((role,value));return self.add(value)


def context(artifacts):
    ctx=context_for(artifacts.image_ref,'image.color')
    ctx.lease.command.inputs.append(artifacts.image.alpha)
    ctx.lease.execution_profile.update(resource_estimate={'ram_bytes':536870912},tile_size=256)
    return ctx


@pytest.mark.parametrize('adaptive',[False,True])
def test_color_stage_matches_reference_without_applying_strength(tmp_path,adaptive):
    artifacts=Artifacts(tmp_path,adaptive);root=tmp_path/'scratch';root.mkdir();ctx=context(artifacts)
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch;result=execute(ctx,artifacts)
    diagnostic=ColorDiagnostics()
    expected=trilinear(artifacts.grid,artifacts.rgb) if adaptive else apply_global(artifacts.rgb,artifacts.config,diagnostic)
    assert np.array_equal(artifacts.output,expected)
    assert not np.array_equal(artifacts.output,artifacts.rgb) # strength zero belongs to the later local stage
    image=artifacts.staged[0][1];report=artifacts.staged[1][1]
    assert image.alpha==artifacts.image.alpha and image.canvas==artifacts.image.canvas and image.transform_chain==artifacts.image.transform_chain
    assert report.strength_applied is False and len(result.outputs)==3
    if not adaptive:
        for stage in report.clipping:
            assert stage.above_one==pytest.approx(diagnostic.clipping[stage.stage]['above_one'],abs=1e-15)
            assert stage.below_zero==pytest.approx(diagnostic.clipping[stage.stage]['below_zero'],abs=1e-15)
    assert ctx.snapshot_progress()==dict(done=5,total=5)


def test_color_rejects_undeclared_alpha_before_staging(tmp_path):
    artifacts=Artifacts(tmp_path,False);ctx=context(artifacts);ctx.lease.command.inputs=[]
    with pytest.raises(OperationFailure) as error:execute(ctx,artifacts)
    assert error.value.error.code=='INPUT_INVALID' and not artifacts.uploads


def test_cancelled_color_removes_unpublished_output(tmp_path):
    artifacts=Artifacts(tmp_path,False);root=tmp_path/'scratch';root.mkdir();ctx=context(artifacts)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==7:raise RuntimeError('cancelled')
    ctx.check_active=active
    with ScratchSpace(root,uuid4(),uuid4(),1,1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        with pytest.raises(RuntimeError,match='cancelled'):execute(ctx,artifacts)
        assert list(scratch.directory.iterdir())==[] and not artifacts.uploads
