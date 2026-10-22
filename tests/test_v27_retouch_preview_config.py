"""Explicit own CPU startup; no SQL or photographic qualification performed."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime import retouch_preview_config as module
from photo_worker.runtime.service import WorkerConfigurationError


def env(values):return lambda key,default=None:values.get(key,default)


def test_retouch_disabled_does_not_read_storage_or_runtime():
    assert module.retouch_preview_from_environment(SimpleNamespace(),env({})) is None
    assert module.retouch_preview_from_environment(SimpleNamespace(),env({'PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED':'true'})) is None


@pytest.mark.parametrize('values',[
    {'PHOTO_RETOUCH_PREVIEW_ENABLED':'1'},
    {'PHOTO_RETOUCH_PREVIEW_ENABLED':'true'},
    {'PHOTO_RETOUCH_PREVIEW_ENABLED':'true','PHOTO_RETOUCH_PREVIEW_MAX_RAM_BYTES':'01'},
    {'PHOTO_RETOUCH_PREVIEW_ENABLED':'true','PHOTO_RETOUCH_PREVIEW_MAX_RAM_BYTES':'9007199254740992'},
    {'PHOTO_RETOUCH_PREVIEW_ENABLED':'true','PHOTO_RETOUCH_PREVIEW_MAX_RAM_BYTES':'32000000000'},
])
def test_retouch_startup_refuses_missing_pins_and_invalid_ram(values):
    config = SimpleNamespace(resource_class='cpu',backend='cpu',icc_root=None)
    with pytest.raises(WorkerConfigurationError):module.retouch_preview_from_environment(config,env(values))


@pytest.mark.parametrize('resource,backend',[('gpu','cuda'),('gpu','mps'),('media','cpu')])
def test_retouch_startup_requires_own_cpu_worker(resource,backend):
    with pytest.raises(WorkerConfigurationError,match='CPU worker'):
        module.retouch_preview_from_environment(SimpleNamespace(resource_class=resource,backend=backend),
            env({'PHOTO_RETOUCH_PREVIEW_ENABLED':'true'}))


def test_retouch_startup_constructs_factory_with_exact_measured_pins(tmp_path,monkeypatch):
    from photo_worker.operations.semantic import exposure_color
    values = {'PHOTO_RETOUCH_PREVIEW_ENABLED':'true','PHOTO_RETOUCH_PREVIEW_MAX_RAM_BYTES':'32000000000',
        'S3_ENDPOINT':'https://storage.example.test','S3_BUCKET':'photos','S3_ACCESS_KEY':'test','S3_SECRET_KEY':'test'}
    for key in ('MODEL_LOCK_SHA','RUNTIME_LOCK_SHA','DEVICE_PROFILE_SHA','DATASET_SHA'):
        values['PHOTO_RETOUCH_PREVIEW_'+key] = 'a'*64
    (tmp_path/'profiles.lock.json').write_bytes(b'{}')
    config = SimpleNamespace(resource_class='cpu',backend='cpu',icc_root=tmp_path,
        database_url='not-connected',development=False)
    monkeypatch.setattr(module,'ICCRegistry',lambda root:SimpleNamespace(resolve=lambda name:SimpleNamespace(sha256='b'*64)))
    monkeypatch.setattr(exposure_color,'local_exposure_icc_verifier',lambda registry:lambda color:True)
    monkeypatch.setattr(module,'TaskDatabase',lambda url:SimpleNamespace())
    captured = {}
    def factory(**kwargs):captured.update(kwargs);return lambda *a:None
    monkeypatch.setattr(module,'sql_retouch_preview_factory',factory)
    startup = module.retouch_preview_from_environment(config,env(values))
    assert isinstance(startup,module.RetouchPreviewStartup)
    assert captured['max_ram_bytes'] == 32000000000 and captured['dataset_sha'] == 'a'*64
    assert startup.presentation.icc_root == tmp_path.resolve() and startup.presentation.srgb_sha == 'b'*64


@pytest.mark.parametrize('resource,backend',[('cpu','cpu'),('gpu','mps')])
def test_service_rejects_invalid_retouch_startup_before_database(monkeypatch,resource,backend):
    from photo_worker.runtime import service
    def forbidden(*a,**k):raise AssertionError('DB opened before startup validation')
    monkeypatch.setattr(service,'TaskDatabase',forbidden)
    config = SimpleNamespace(resource_class=resource,backend=backend,interchange_enabled=False,delivery_enabled=False)
    with pytest.raises(WorkerConfigurationError,match='Retouch preview installation'):
        service.build_worker(config,retouch_preview_startup=object())
