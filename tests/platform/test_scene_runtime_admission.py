"""Actual scene entry refuses pinned work without an installed authority."""
from types import SimpleNamespace
import pytest
from photo_worker.operations.scene.parse import execute,_required_scene_ram
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scene_config import configured_scene_runtime


def test_pinned_scene_refuses_before_params_or_model_loading():
    class Artifacts:
        def params(self,*args):raise AssertionError('unqualified task reached artifact IO')
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(backend='cuda',
        resource_estimate={'ram_bytes':2**30},model_lock_sha='a'*64)),check_active=lambda:None)
    with pytest.raises(OperationFailure,match='SCENE_RUNTIME_INSTALLATION_REQUIRED'):
        execute(ctx,Artifacts(),None,'cuda')


def test_live_scene_authority_refusal_precedes_model_loading():
    class Artifacts:
        def params(self,*args):raise AssertionError('refused task reached IO')
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(backend='cuda',
        resource_estimate={'ram_bytes':2**30},runtime_lock_sha='b'*64)),check_active=lambda:None)
    with pytest.raises(OperationFailure,match='SCENE_RUNTIME_UNQUALIFIED'):
        execute(ctx,Artifacts(),None,'cuda',lambda _:False)


def test_scene_pins_cannot_be_implicitly_enabled():
    env={'PHOTO_SCENE_MODEL_LOCK_SHA':'a'*64}
    with pytest.raises(ValueError,match='CONFIGURATION_INVALID'):
        configured_scene_runtime(SimpleNamespace(),None,lambda key,default='':env.get(key,default))


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_scene_runtime_disabled_preserves_existing_unpinned_installation(backend):
    assert configured_scene_runtime(SimpleNamespace(backend=backend),None,lambda key,default='':default) is None


def test_scene_decode_peak_is_reserved_even_for_tiny_source_canvas():
    assert _required_scene_ram(32,32)>=3*1024**3+512*1024**2+32*32*128
    assert _required_scene_ram(64,64)-_required_scene_ram(32,32)==(64*64-32*32)*128
