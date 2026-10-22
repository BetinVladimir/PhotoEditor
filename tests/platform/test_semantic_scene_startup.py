"""Ordinary production constructor refusal/ownership; authored, not executed."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime import semantic_scene_config as worker
from photo_worker.v2 import semantic_scene_config as adopter


def test_semantic_scene_disabled_does_not_load_runtime_or_objects(monkeypatch):
    def forbidden(*args,**kwargs):raise AssertionError('disabled constructor performed IO')
    monkeypatch.setattr(worker,'verify_scene_runtime',forbidden)
    assert worker.semantic_scene_from_environment(SimpleNamespace(),lambda key,default='':default) is None
    assert adopter.semantic_scene_adopter_from_environment(SimpleNamespace(),lambda key,default='':default) is None


@pytest.mark.parametrize('backend,resource',[('mps','gpu'),('cpu','gpu'),('cuda','cpu')])
def test_semantic_scene_never_claims_unimplemented_backend(monkeypatch,backend,resource):
    def forbidden(env):raise AssertionError('unsupported worker performed CUDA installation IO')
    monkeypatch.setattr(worker,'scene_operator_configuration',forbidden)
    assert worker.semantic_scene_from_environment(SimpleNamespace(backend=backend,resource_class=resource),
        lambda key,default='':'true' if key=='PHOTO_SEMANTIC_TRANSFER_ENABLED' else default) is None


def test_semantic_worker_invalid_enablement_refused_even_on_metal():
    with pytest.raises(ValueError,match='SEMANTIC_ENABLEMENT_INVALID'):
        worker.semantic_scene_from_environment(SimpleNamespace(backend='mps',resource_class='gpu'),
            lambda key,default='':'yes' if key=='PHOTO_SEMANTIC_TRANSFER_ENABLED' else default)



@pytest.mark.parametrize('raw',['','0','-1','1.5','9007199254740992'])
def test_semantic_adopter_requires_explicit_bounded_ram(monkeypatch,raw):
    monkeypatch.setattr(adopter,'scene_operator_configuration',lambda env:({},lambda active:None))
    with pytest.raises(ValueError):
        adopter.semantic_scene_adopter_from_environment(SimpleNamespace(),lambda key,default='':raw)


def test_semantic_adopter_keeps_installed_pin_and_does_not_fake_result_scope(monkeypatch,tmp_path):
    pins={name:character*64 for name,character in [('model_lock_sha','a'),('runtime_lock_sha','b'),
        ('device_profile_sha','c'),('dataset_sha','d')]}
    verify=lambda active:active()
    monkeypatch.setattr(adopter,'scene_operator_configuration',lambda env:(pins,verify))
    config=SimpleNamespace(layer_scratch_root=tmp_path,layer_scratch_bytes=2**30)
    installed,ram=adopter.semantic_scene_adopter_from_environment(config,lambda key,default='':str(2**30))
    pins['runtime_lock_sha']='e'*64
    assert installed.measurements['runtime_lock_sha']=='b'*64 and ram==2**30
    with pytest.raises(ValueError,match='RESULT_SCOPE_REQUIRED'):installed()
