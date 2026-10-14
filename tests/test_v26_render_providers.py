from contextlib import contextmanager
from dataclasses import replace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.render_layers import RenderLayer
from photo_worker.v2.render_layer_params import LoadedRenderLayer,RenderLayerOperator
from photo_worker.v2.render_providers import RenderLayerProvider,RenderLayerProviders


def setup():
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    layer=RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='test.op',
        operation_version='1',params=ref,mask={'type':'all'})
    calls=[]
    @contextmanager
    def prepare(item,current):
        calls.append('prepare');item.layer.id=uuid4()
        try:yield current
        finally:calls.append('closed')
    operator=RenderLayerOperator('test.op','1','b'*64,ArtifactRef,
        lambda:calls.append('verify'),lambda params,inputs:None)
    return LoadedRenderLayer(layer,ref,'b'*64),RenderLayerProvider(operator,prepare),calls


def test_inventory_drives_dispatch_and_retains_caller_metadata():
    item,provider,calls=setup();original=item.layer.id;marker=object()
    registry=RenderLayerProviders((provider,))
    assert registry.operators==(provider.operator,)
    with registry(item,marker) as result:
        assert result is marker and calls==['verify','prepare']
    assert item.layer.id==original and calls==['verify','prepare','closed']


@pytest.mark.parametrize('change',[{'runtime_sha256':'c'*64},
    {'params':RenderLayer(id=uuid4(),enabled=False,opacity=0,blend='normal',operation='x',
        operation_version='1',params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),mask={})}])
def test_runtime_or_params_substitution_never_calls_provider(change):
    item,provider,calls=setup();registry=RenderLayerProviders((provider,))
    with pytest.raises(ValueError,match='BINDING_CHANGED'):
        with registry(replace(item,**change),None):pass
    assert calls==[]


def test_unknown_operation_and_duplicate_inventory_fail():
    item,provider,calls=setup()
    with pytest.raises(ValueError,match='INVENTORY_INVALID'):
        RenderLayerProviders((provider,provider))
    with pytest.raises(ValueError,match='UNAVAILABLE'):
        with RenderLayerProviders(())(item,None):pass
    assert calls==[]


def test_cancellation_after_yield_closes_provider():
    item,provider,calls=setup();cancelled=False
    def check():
        if cancelled:raise RuntimeError('cancelled')
    registry=RenderLayerProviders((provider,),check_active=check)
    with pytest.raises(RuntimeError,match='cancelled'):
        with registry(item,None):cancelled=True
    assert calls==['verify','prepare','closed']


def test_shared_assembly_owns_inputs_and_unifies_worker_replay_inventory():
    from types import SimpleNamespace
    from photo_worker.v2.render_providers import RenderProviderAssembly
    item,provider,calls=setup()
    source={'version':'pinned'};profile={'ram':123}
    configured={'supported_modules':frozenset({'color'}),
        **{name:1024 for name in ('max_params_document_bytes','max_override_document_bytes',
            'max_input_bytes','max_mask_bytes','max_tile_bytes')}}
    observed=[]
    def base(artifacts,inputs,costs,active):
        observed.append((inputs.copy(),costs.copy()))
        inputs['version']='changed';costs['ram']=0
        return lambda *args:None
    def layers(artifacts,inputs,costs,active):
        assert inputs==source and costs==profile
        return (provider,)
    measure_calls=[]
    def measure(*args):
        measure_calls.append(None)
        return lambda *args:None
    assembly=RenderProviderAssembly(base_factory=base,layer_factory=layers,
        override_factory=lambda *args:(),measure_factory=measure,preparation=configured)
    configured['max_input_bytes']=1
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=profile),check_active=lambda:None)
    worker=assembly.worker(ctx,object(),source)
    replay=assembly.replay(object(),source,SimpleNamespace(execution_profile=lambda:profile),check_active=lambda:None)
    assert worker['operators']==worker['render_proposal'].operators==replay['operators']
    assert len(measure_calls)==1 and 'measure' not in replay
    worker['preparation']['max_input_bytes']=2
    assert replay['preparation']['max_input_bytes']==1024
    assert source=={'version':'pinned'} and profile=={'ram':123}
    assert observed==[(source,profile),(source,profile)]


def test_combined_native_factories_refuse_duplicates_and_isolate_metadata():
    from photo_worker.v2.render_providers import combine_render_layer_factories
    _,provider,_=setup()
    state={'source':'pinned'};profile={'ram':123}
    def first(artifacts,inputs,costs,active):
        inputs['source']='changed';costs['ram']=0
        return (provider,)
    def second(artifacts,inputs,costs,active):
        assert inputs==state and costs==profile
        return ()
    providers=combine_render_layer_factories(first,second)(object(),state,profile,lambda:None)
    assert providers==(provider,) and state=={'source':'pinned'} and profile=={'ram':123}
    with pytest.raises(ValueError,match='INVENTORY_INVALID'):
        combine_render_layer_factories(first,first)(object(),state,profile,lambda:None)


def test_combined_native_factories_pin_one_snapshot_for_all_components():
    from photo_worker.v2.render_providers import combine_render_layer_factories
    state={'source':'pinned'};profile={'ram':123}
    def first(*args):
        state['source']='changed';profile['ram']=0
        return ()
    def second(artifacts,inputs,costs,active):
        assert inputs=={'source':'pinned'} and costs=={'ram':123}
        return ()
    assert combine_render_layer_factories(first,second)(object(),state,profile,lambda:None)==()


def test_three_concrete_native_factories_share_exact_dispatch_inventory():
    from types import SimpleNamespace
    from photo_worker.v2.render_denoise import PinnedDenoiseProviderFactory
    from photo_worker.v2.render_detail import PinnedDetailProviderFactory
    from photo_worker.v2.render_defects import PinnedDefectProviderFactory
    from photo_worker.v2.render_providers import combine_render_layer_factories,RenderProviderAssembly
    from photo_worker.contracts.restoration import DenoiseParams
    from photo_worker.contracts.portrait_detail import PortraitParams
    from photo_worker.contracts.defects import DefectRepairParams
    pool=SimpleNamespace(lock=SimpleNamespace(lock_id='a'*64))
    protection=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    trusted=dict(runtime_sha256='c'*64,verify_runtime=lambda:None,validate_binding=lambda *args:None)
    denoise=PinnedDenoiseProviderFactory(pool,backend='cuda',
        provider_peak=dict(ram_bytes=1,scratch_bytes=0,vram_bytes=0,output_bytes=0),**trusted)
    detail=PinnedDetailProviderFactory(**trusted)
    defects=PinnedDefectProviderFactory(protection_reference=lambda state:protection,**trusted)
    assembly=RenderProviderAssembly(base_factory=lambda *args:lambda *args:None,
        layer_factory=combine_render_layer_factories(denoise,detail,defects),
        override_factory=lambda *args:(),measure_factory=lambda *args:lambda *args:None,
        preparation={'supported_modules':frozenset({'color'}),
            **{name:1024 for name in ('max_params_document_bytes','max_override_document_bytes',
                'max_input_bytes','max_mask_bytes','max_tile_bytes')}})
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend':'cuda'}),check_active=lambda:None)
    loaded=assembly.worker(ctx,object(),object())
    operators=loaded['operators']
    assert operators==loaded['render_proposal'].operators
    assert [(op.operation,op.params_model) for op in operators]==[
        ('image.denoise',DenoiseParams),('portrait.detail',PortraitParams),('defects.repair',DefectRepairParams)]


def test_assembly_pins_snapshot_before_any_component_callback():
    from types import SimpleNamespace
    from photo_worker.v2.render_providers import RenderProviderAssembly
    source={'source':'pinned'};profile={'ram':123}
    def base(*args):
        source['source']='changed';profile['ram']=0
        return lambda *args:None
    def layers(artifacts,inputs,costs,active):
        assert inputs=={'source':'pinned'} and costs=={'ram':123}
        return ()
    assembly=RenderProviderAssembly(base_factory=base,layer_factory=layers,
        override_factory=lambda *args:(),measure_factory=lambda *args:lambda *args:None,
        preparation={'supported_modules':frozenset({'color'}),
            **{name:1024 for name in ('max_params_document_bytes','max_override_document_bytes',
                'max_input_bytes','max_mask_bytes','max_tile_bytes')}})
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=profile),check_active=lambda:None)
    assert assembly.worker(ctx,object(),source)['operators']==()
