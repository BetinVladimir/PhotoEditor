from types import SimpleNamespace
import pytest
from photo_worker.runtime.background_cleanup_preview_config import background_cleanup_preview_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def test_background_cleanup_preview_disabled_needs_no_model_or_storage():
    assert background_cleanup_preview_from_environment(SimpleNamespace(),lambda key,default=None:default) is None


@pytest.mark.parametrize('backend,resource',[('cpu','cpu'),('metal','gpu'),('cuda','cpu')])
def test_background_cleanup_preview_requires_supported_gpu_class_worker(backend,resource):
    config=SimpleNamespace(backend=backend,resource_class=resource)
    with pytest.raises(WorkerConfigurationError,match='GPU-class worker'):
        background_cleanup_preview_from_environment(config,lambda key,default=None:'true' if key=='PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED' else default)


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_background_cleanup_preview_supported_backend_requires_own_pins(backend):
    config=SimpleNamespace(backend=backend,resource_class='gpu',icc_root=None)
    with pytest.raises(WorkerConfigurationError,match='measured runtime hashes'):
        background_cleanup_preview_from_environment(config,lambda key,default=None:'true' if key=='PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED' else default)


def test_background_cleanup_preview_rejects_ambiguous_enablement():
    with pytest.raises(WorkerConfigurationError):
        background_cleanup_preview_from_environment(SimpleNamespace(),lambda key,default=None:'1')


def test_removal_enablement_does_not_enable_background_cleanup():
    values={'PHOTO_REMOVAL_PREVIEW_ENABLED':'true',
            'PHOTO_REMOVAL_PREVIEW_MODEL_LOCK_SHA':'a'*64}
    assert background_cleanup_preview_from_environment(SimpleNamespace(),
        lambda key,default=None:values.get(key,default)) is None


@pytest.mark.parametrize('resource,backend,factory',[
    ('cpu','cpu',lambda *args:None),
    ('gpu','metal',lambda *args:None),
    ('gpu','cuda',object()),
    ('gpu','mps',lambda *args:None),
])
def test_background_worker_configuration_denied_before_db_io(monkeypatch,resource,backend,factory):
    from photo_worker.runtime import service
    calls=[]
    def forbidden(*args,**kwargs):
        calls.append(True)
        raise AssertionError('DB constructed before installation validation')
    monkeypatch.setattr(service,'TaskDatabase',forbidden)
    config=SimpleNamespace(resource_class=resource,backend=backend,model_root=None,model_lock=None,interchange_enabled=False,delivery_enabled=False)
    with pytest.raises(WorkerConfigurationError,match='matching installed model worker'):
        service.build_worker(config,background_cleanup_preview_installation_factory=factory)
    assert calls==[]
