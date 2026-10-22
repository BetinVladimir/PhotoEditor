from types import SimpleNamespace
import pytest
from photo_worker.runtime.person_config import person_runtime_locks_from_env,configured_person_runtime
from photo_worker.runtime.service import WorkerConfigurationError


def test_person_configuration_is_explicit_and_complete():
    assert person_runtime_locks_from_env({},'media','cpu',False) is None
    env=dict(PHOTO_PERSON_RUNTIME_ENABLED='true',PHOTO_PERSON_MODEL_LOCK_SHA='a'*64,PHOTO_PERSON_RUNTIME_LOCK_SHA='b'*64,PHOTO_PERSON_DEVICE_PROFILE_SHA='c'*64)
    assert person_runtime_locks_from_env(env,'gpu','cuda',True)==('a'*64,'b'*64,'c'*64)
    for patch in ({'PHOTO_PERSON_RUNTIME_ENABLED':'yes'},{'PHOTO_PERSON_RUNTIME_LOCK_SHA':''},{'PHOTO_PERSON_RUNTIME_ENABLED':'false'}):
        with pytest.raises(WorkerConfigurationError):person_runtime_locks_from_env(dict(env,**patch),'gpu','cuda',True)
    with pytest.raises(WorkerConfigurationError):person_runtime_locks_from_env(env,'cpu','cpu',True)
    with pytest.raises(WorkerConfigurationError):person_runtime_locks_from_env(env,'gpu','cuda',False)


def test_person_startup_measures_before_returning_authority(monkeypatch):
    import photo_worker.operations.mask.person_measurement as provider
    config=SimpleNamespace(person_runtime_locks=('a'*64,'b'*64,'c'*64),backend='cuda')
    actual=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,backend='cuda')
    calls=[]
    def measure(pool,backend):calls.append(backend);return actual
    monkeypatch.setattr(provider,'installed_person_measurements',measure)
    assert callable(configured_person_runtime(config,object())) and calls==['cuda']
    actual['runtime_lock_sha']='d'*64
    with pytest.raises(WorkerConfigurationError):configured_person_runtime(config,object())


def test_build_worker_installs_measured_person_authority(monkeypatch,tmp_path):
    from contextlib import contextmanager
    import ssl
    from uuid import uuid4
    import photo_worker.runtime.service as service
    import photo_worker.models.lock as locks
    import photo_worker.models.pool as pools
    import photo_worker.models.adapters as adapters
    import photo_worker.operations.mask.person as person
    import photo_worker.operations.mask.person_measurement as provider
    from photo_worker.runtime.executor import OperationResult
    model_file=tmp_path/'model-lock.json';model_file.write_text('{}')
    config=service.WorkerConfig(database_url='postgres://unused',amqp_url='amqps://unused',worker_id=uuid4(),resource_class='gpu',scratch_root=tmp_path,gateway_origin='https://unused',ca=tmp_path/'ca',certificate=tmp_path/'cert',private_key=tmp_path/'key',max_scratch_bytes=1024,max_download_bytes=1024,max_upload_bytes=1024,backend='cuda',model_root=tmp_path,model_lock=model_file,person_runtime_locks=('a'*64,'b'*64,'c'*64))
    model_lock=SimpleNamespace(lock_id='a'*64,models=[SimpleNamespace(id='lraspp_mobilenet_v3_large')])
    monkeypatch.setattr(locks.ModelLock,'model_validate_json',lambda raw:model_lock)
    monkeypatch.setattr(pools,'ModelPool',lambda *args:SimpleNamespace(lock=model_lock))
    monkeypatch.setattr(adapters,'factories',lambda:{})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    measured=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,backend='cuda')
    calls=[]
    def measure(pool,backend):calls.append(backend);return measured
    monkeypatch.setattr(provider,'installed_person_measurements',measure)
    @contextmanager
    def artifacts(*args):yield object()
    monkeypatch.setattr(person.TaskArtifacts,'create',artifacts)
    executed=[]
    def execute(ctx,artifacts,pool,backend,authority):
        assert authority(ctx) is True
        executed.append(backend)
        return OperationResult(outputs=[],bytes_written=0)
    monkeypatch.setattr(person,'execute',execute)
    _,transport=service.build_worker(config)
    assert calls==['cuda'] and 'mask.person' in transport.executor.operations
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(measured)),check_active=lambda:None)
    transport.executor.operations['mask.person'].execute(ctx)
    assert calls==['cuda','cuda'] and executed==['cuda']
    measured['runtime_lock_sha']='d'*64
    with pytest.raises(WorkerConfigurationError):service.build_worker(config)


@pytest.fixture(autouse=True)
def installed_startup_schema(monkeypatch):
    # Constructor isolation only; installed SQL authority has separate tests.
    from photo_worker.runtime.database import TaskDatabase
    monkeypatch.setattr(TaskDatabase, 'media_renderer_policy_ready', lambda self: True)
    monkeypatch.setattr(TaskDatabase, 'task_result_fence_ready', lambda self: True)
    monkeypatch.setattr(TaskDatabase, 'editor_internal_dispatch_installed', lambda self, operation: True)
