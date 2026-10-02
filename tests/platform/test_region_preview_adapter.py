from contextlib import contextmanager
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.region.adapter import execute,RenderedRegionCanvas,region_operations
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_region_preview_presentation import fixture,ROOT,ICC_LOCK
from test_region_preview_stage import Artifacts as Staging
from test_style_resolve import profile


class Artifacts(Staging):
    def __init__(self,scratch,params,body):super().__init__(scratch);self.value=params;self.body=body;self.reads=[]
    def params(self,model):self.reads.append('params');return self.value
    def contract(self,ref,model):self.reads.append('recipe');return self.value.frozen_recipe
    @contextmanager
    def stream(self,ref):self.reads.append('source');yield BytesIO(self.body)
    def style(self,ref):self.reads.append('style');return profile()


class Renderer:
    supported_modules=frozenset({'color','strength','adaptive','local','beauty','defects','geometry','culling','series','composition','portrait_detail','inpainting','generation','masking','restoration','background','region_adjustments'})
    runtime_sha256='a'*64
    backend='cpu';renderer_version='v2.2';icc_lock_sha=ICC_LOCK;icc_root=ROOT;model_lock_sha='e'*64
    def __init__(self):self.events=[]
    def verify_runtime(self):self.events.append('verify')
    def admit_resources(self,ctx,params):self.events.append('resources')
    @contextmanager
    def render(self,ctx,artifacts,params,source,style):
        self.events.append('render')
        assert (source.canvas.width,source.canvas.height)==(400,300)
        yield RenderedRegionCanvas(np.full((300,400,3),.5,np.float32),None)
        self.events.append('release')


class Admission:
    def __init__(self,deny=False):self.deny=deny;self.calls=0
    def admit(self,ctx,params):
        self.calls+=1
        if self.deny:raise ValueError('not admitted')


def context(renderer):
    return SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(resource_estimate=dict(pixel_count=120000,ram_bytes=32000000,scratch_bytes=8000000),region_runtime_sha256=renderer.runtime_sha256,backend=renderer.backend,renderer_version=renderer.renderer_version,icc_lock_sha=renderer.icc_lock_sha,model_lock_sha=renderer.model_lock_sha)),check_active=lambda:None,progress=lambda *_:None)


def test_adapter_orchestrates_without_claiming_real_render_qualification(tmp_path):
    params,body=fixture();params.source.bytes=len(body)
    renderer=Renderer();admission=Admission()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch,params,body)
        result=execute(context(renderer),artifacts,admission,renderer)
        assert len(result.outputs)==2 and admission.calls==1
        assert renderer.events==['verify','resources','render','release']
        assert artifacts.reads==['params','recipe','source','style']


def test_admission_denial_prevents_source_and_output_io(tmp_path):
    params,body=fixture();renderer=Renderer()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch,params,body)
        with pytest.raises(OperationFailure):execute(context(renderer),artifacts,Admission(deny=True),renderer)
        assert artifacts.reads==['params'] and artifacts.outputs=={}
        assert renderer.events==['verify']


def test_adapter_missing_runtime_or_bad_profile_rejected_before_input_io():
    renderer=Renderer();ctx=context(renderer)
    with pytest.raises(OperationFailure):execute(ctx,None,None,renderer)
    ctx.lease.execution_profile['model_lock_sha']='f'*64
    with pytest.raises(OperationFailure):execute(ctx,None,Admission(),renderer)
    assert renderer.events==[]
    with pytest.raises(ValueError):region_operations(None,None,renderer)


def test_color_only_renderer_cannot_silently_skip_beauty(tmp_path):
    from photo_worker.contracts.style import Beauty
    class BeautyArtifacts(Artifacts):
        def style(self,ref):
            value=super().style(ref)
            data=value.model_dump(mode='json',by_alias=True)
            data['beauty']=dict(enabled=True,algorithm='beautyfilter_v1',smoothing=.2,tone_evenness=.1,texture_keep=1.,skin_mask='face68_conservative_v1',protect_features=True)
            # Existing full StyleV2 validates required model declarations.
            data['models']=[dict(id='sfd',weight_id='test'),dict(id='fan2d',weight_id='test')]
            return type(value).model_validate(data)
    params,body=fixture();params.source.bytes=len(body)
    renderer=Renderer();renderer.supported_modules=frozenset({'color','strength'})
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=BeautyArtifacts(scratch,params,body)
        with pytest.raises(OperationFailure) as rejected:execute(context(renderer),artifacts,Admission(),renderer)
        assert str(rejected.value.__cause__)=="REGION_COMPLETE_RENDERER_REQUIRED"
        assert 'render' not in renderer.events and artifacts.outputs=={}


def test_adaptive_prototype_local_requires_complete_local_renderer(tmp_path):
    from photo_worker.contracts.style import Adaptation, Prototype, LUT, LocalColor, ModelReference, StyleV2
    from photo_worker.operations.region.adapter import required_region_modules
    style=profile()
    prototype=Prototype(id='local-reference',features=[0.]*20,
        absolute_lut=LUT(kind='absolute',size=33,encoding='constant',values=[.5,.5,.5]),
        local=LocalColor(enabled=True,person_ev=.2,background_ev=0.,person_saturation=1.,background_saturation=1.,mask_policy_id="person_guided_v1"))
    adaptation=Adaptation(feature_version='stats20_v1',mean=[0.]*20,scale=[.05]*20,k=1,temperature=1,ood_threshold=1.,prototypes=[prototype],coverage_bins=[True]*512)
    raw=style.model_dump(mode='json',by_alias=True)
    raw.update(mode='adaptive',adaptation=adaptation.model_dump(mode='json'),strength=0.,models=[ModelReference(id='lraspp_mobilenet_v3_large',weight_id='test').model_dump(mode='json')])
    style=StyleV2.model_validate(raw)
    assert not style.local.enabled
    assert {'adaptive','local','color','strength'} <= required_region_modules(style)

    class AdaptiveArtifacts(Artifacts):
        def style(self,ref):
            self.reads.append('style')
            return style
    renderer=Renderer()
    renderer.supported_modules=frozenset({'color','strength','adaptive'})
    params,body=fixture()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=AdaptiveArtifacts(scratch,params,body)
        with pytest.raises(OperationFailure,match='INPUT_INVALID'):
            execute(context(renderer),artifacts,Admission(),renderer)
        assert 'render' not in renderer.events and artifacts.outputs=={}


@pytest.mark.parametrize('runtime_pin',[None,'f'*64,'A'*64,'invalid'])
def test_runtime_digest_mismatch_rejected_before_verification_or_input_io(runtime_pin):
    renderer=Renderer()
    ctx=context(renderer)
    ctx.lease.execution_profile['region_runtime_sha256']=runtime_pin
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        execute(ctx,None,Admission(),renderer)
    assert renderer.events==[]


def test_renderer_without_runtime_digest_cannot_register_cuda_operation():
    renderer=Renderer()
    renderer.backend='cuda'
    renderer.runtime_sha256=None
    with pytest.raises(ValueError,match='runtime digest'):
        region_operations(None,Admission(),renderer)
    assert renderer.events==[]


@pytest.mark.parametrize('key,value,code',[
    ('pixel_count',1,'CONFIG_INVALID'),
    ('ram_bytes',1,'RAM_ADMISSION_EXCEEDED'),
    ('scratch_bytes',1,'SCRATCH_ADMISSION_EXCEEDED'),
    ('ram_bytes',True,'CONFIG_INVALID'),
])
def test_provider_cannot_waive_minimum_source_budget(tmp_path,key,value,code):
    params,body=fixture()
    params.source.bytes=len(body)
    renderer=Renderer()
    ctx=context(renderer)
    ctx.lease.execution_profile['resource_estimate'][key]=value
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch,params,body)
        with pytest.raises(OperationFailure,match=code):
            execute(ctx,artifacts,Admission(),renderer)
        assert artifacts.reads==['params','recipe','source']
        assert renderer.events==['verify','resources']
        assert artifacts.outputs=={}


def test_renderer_resource_failure_keeps_public_failure_code(tmp_path):
    class BudgetRenderer(Renderer):
        def admit_resources(self,ctx,params):
            raise OperationFailure('VRAM_ADMISSION_EXCEEDED')
    renderer=BudgetRenderer()
    params,body=fixture()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch,params,body)
        with pytest.raises(OperationFailure,match='VRAM_ADMISSION_EXCEEDED'):
            execute(context(renderer),artifacts,Admission(),renderer)
        assert artifacts.reads==['params'] and artifacts.outputs=={}


@pytest.mark.parametrize('stage,reads',[
    ('runtime',[]),
    ('params',['params']),
    ('recipe',['params','recipe']),
    ('style',['params','recipe','source','style']),
])
@pytest.mark.parametrize('failure',['cancel','lease'])
def test_cancelled_preparation_never_enters_renderer(tmp_path,stage,reads,failure):
    from photo_worker.runtime.executor import TaskCancelled, LeaseLost
    inactive=False
    class StoppingRenderer(Renderer):
        def verify_runtime(self):
            nonlocal inactive
            super().verify_runtime()
            if stage=='runtime':inactive=True
    class StoppingArtifacts(Artifacts):
        def params(self,model):
            nonlocal inactive
            value=super().params(model)
            if stage=='params':inactive=True
            return value
        def contract(self,ref,model):
            nonlocal inactive
            value=super().contract(ref,model)
            if stage=='recipe':inactive=True
            return value
        def style(self,ref):
            nonlocal inactive
            value=super().style(ref)
            if stage=='style':inactive=True
            return value
    renderer=StoppingRenderer()
    ctx=context(renderer)
    error=TaskCancelled if failure=='cancel' else LeaseLost
    def active():
        if inactive:raise error('stopped')
    ctx.check_active=active
    params,body=fixture()
    params.source.bytes=len(body)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=StoppingArtifacts(scratch,params,body)
        admission=Admission()
        with pytest.raises(error):execute(ctx,artifacts,admission,renderer)
        if stage in ("runtime","params"):assert admission.calls==0
        assert artifacts.reads==reads and artifacts.outputs=={}
        assert 'render' not in renderer.events


@pytest.mark.parametrize('when',['enter','release'])
@pytest.mark.parametrize('failure',['cancel','lease'])
def test_renderer_context_releases_and_never_stages_after_stop(tmp_path,when,failure):
    from photo_worker.runtime.executor import TaskCancelled, LeaseLost
    inactive=False
    class OwnedRenderer(Renderer):
        @contextmanager
        def render(self,ctx,artifacts,params,source,style):
            nonlocal inactive
            self.events.append('render')
            if when=='enter':inactive=True
            try:
                yield RenderedRegionCanvas(np.full((300,400,3),.5,np.float32),None)
            finally:
                self.events.append('release')
                if when=='release':inactive=True
    renderer=OwnedRenderer()
    ctx=context(renderer)
    error=TaskCancelled if failure=='cancel' else LeaseLost
    def active():
        if inactive:raise error('stopped')
    ctx.check_active=active
    params,body=fixture()
    params.source.bytes=len(body)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch,params,body)
        with pytest.raises(error):execute(ctx,artifacts,Admission(),renderer)
        assert renderer.events==['verify','resources','render','release']
        assert artifacts.outputs=={}
        # Cancellation must leave the attempt scratch capacity usable.
        with scratch.reserve(8*1024*1024):pass
