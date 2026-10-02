from uuid import uuid4
import pytest
from typer.testing import CliRunner
from photo_worker.cli import app
from photo_worker.runtime.service import WorkerConfig,WorkerConfigurationError


def environment(tmp_path):
    return dict(PHOTO_WORKER_ID=str(uuid4()),PHOTO_WORKER_CLASS='media',DATABASE_URL='postgres://user:secret@example.test/database',AMQP_URL='amqps://user:secret@example.test',PHOTO_SCRATCH_ROOT=str(tmp_path),GATEWAY_ORIGIN='https://localhost:8443',GATEWAY_CA_FILE='ca.pem',WORKER_CERT_FILE='worker.pem',WORKER_KEY_FILE='worker.key',WORKER_MAX_SCRATCH_BYTES='4096',WORKER_MAX_DOWNLOAD_BYTES='4096',WORKER_MAX_UPLOAD_BYTES='4096')

def test_worker_config_limits_and_secret_redaction(tmp_path):
    env=environment(tmp_path);config=WorkerConfig.from_env(env)
    assert config.resource_class=='media' and 'secret' not in repr(config)
    for key,value in [('PHOTO_WORKER_ID','bad'),('PHOTO_WORKER_CLASS','gpu'),('WORKER_MAX_UPLOAD_BYTES','0'),('WORKER_MAX_SCRATCH_BYTES','9007199254740992'),('WORKER_MAX_DOWNLOAD_BYTES','1.5'),('APP_ENV','unknown'),('GATEWAY_ORIGIN','http://localhost'),('GATEWAY_ORIGIN','https://user:secret@example.test/path')]:
        with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,key:value})
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,'PHOTO_SCRATCH_ROOT':str(tmp_path/'missing')})


def test_archive_worker_class_is_explicitly_supported(tmp_path):
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'archive'})
    assert config.resource_class=='archive' and config.backend=='cpu'


def test_learning_fit_is_registered_only_on_cpu_worker(monkeypatch,tmp_path):
    import ssl
    import photo_worker.runtime.service as service
    from photo_worker.operations.learning.fit import learning_fit_operations
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    assert [op.name for op in learning_fit_operations(None)]==['learning.fit']
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    _,transport=service.build_worker(config)
    assert 'learning.fit' in transport.executor.operations
    assert transport.executor.operations['learning.fit'].resource_class=='cpu'


def test_worker_cli_does_not_expose_dependency_secrets(monkeypatch,tmp_path):
    import photo_worker.runtime.service as service
    monkeypatch.setattr(service.WorkerConfig,'from_env',lambda:None)
    def fail(config):raise RuntimeError('password=private-test-secret')
    monkeypatch.setattr(service,'run_worker',fail)
    result=CliRunner().invoke(app,['run'])
    assert result.exit_code==1 and 'private-test-secret' not in result.output
    assert 'Worker stopped' in result.output


def test_registration_preflight_and_signal_cleanup(monkeypatch,tmp_path):
    import signal
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    state=dict(registered=frozenset({'missing.operation'}),started=False,aborts=0)
    class Database:
        def ping(self):pass
        def heartbeat_cpu_worker(self,*args):return True
        def runtime_operations(self,*args):return state['registered']
    class Executor:
        operations={'asset.verify':object()}
        def abort_active(self):state['aborts']+=1
    class Transport:
        executor=Executor()
        def run(self,stop):
            state['started']=True
            signal.getsignal(signal.SIGTERM)(signal.SIGTERM,None)
            assert stop.is_set()
    monkeypatch.setattr(service,'build_worker',lambda config:(Database(),Transport()))
    config=WorkerConfig.from_env(environment(tmp_path))
    with pytest.raises(WorkerConfigurationError,match='registration'):service.run_worker(config)
    assert not state['started']
    previous=signal.getsignal(signal.SIGTERM)
    state['registered']=frozenset({'asset.verify'})
    service.run_worker(config)
    assert state['started'] and state['aborts']>=1 and signal.getsignal(signal.SIGTERM)==previous


def test_worker_icc_registry_is_explicit_and_validated(tmp_path):
    from pathlib import Path
    from photo_worker.operations.asset.adapters import asset_operations
    root=Path(__file__).resolve().parents[2]/'resources/icc'
    env=environment(tmp_path)
    assert WorkerConfig.from_env(env).icc_root is None
    config=WorkerConfig.from_env({**env,'PHOTO_ICC_ROOT':str(root)})
    assert config.icc_root==root
    assert 'asset.decode' in {op.name for op in asset_operations(None,config.icc_root)}
    assert 'asset.decode' not in {op.name for op in asset_operations(None)}
    with pytest.raises(WorkerConfigurationError,match='PHOTO_ICC_ROOT'):
        WorkerConfig.from_env({**env,'PHOTO_ICC_ROOT':str(tmp_path)})


def test_gpu_backend_is_explicit_and_bound_to_executor(monkeypatch,tmp_path):
    import ssl
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    from photo_worker.runtime.executor import OperationFailure
    env={**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'mps'}
    config=WorkerConfig.from_env(env)
    assert config.backend=='mps' and config.resource_class=='gpu'
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,'PHOTO_WORKER_BACKEND':'auto'})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    _,transport=service.build_worker(config)
    assert set(transport.executor.operations)=={'pair.fit.color'}
    wrong=SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend':'cpu'}))
    with pytest.raises(OperationFailure):transport.executor.operations['pair.fit.color'].execute(wrong)


def test_model_runtime_configuration_is_all_or_nothing(tmp_path):
    env={**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'cpu'}
    with pytest.raises(WorkerConfigurationError,match='must be set together'):
        WorkerConfig.from_env({**env,'PHOTO_MODEL_ROOT':str(tmp_path)})
    lock=tmp_path/'lock.json';lock.write_text('{}')
    with pytest.raises(WorkerConfigurationError,match='PHOTO_MODEL'):
        WorkerConfig.from_env({**env,'PHOTO_MODEL_ROOT':str(tmp_path),'PHOTO_MODEL_LOCK':str(lock)})


def test_gpu_startup_uses_backend_preflight_and_capacity(monkeypatch,tmp_path):
    import signal
    import photo_worker.runtime.service as service
    from photo_worker.runtime.capacity import Capacity
    calls=[]
    class Database:
        def ping(self):pass
        def accelerator_operations(self,worker,backend):
            calls.append(('preflight',backend));return frozenset({'pair.fit.color'})
        def heartbeat_accelerator_worker(self,worker,backend,ops,ram,vram,scratch):
            calls.append(('capacity',backend,ram,vram,scratch));return True
    class Executor:
        operations={'pair.fit.color':object()}
        def abort_active(self):pass
    class Transport:
        executor=Executor()
        def run(self,stop):signal.getsignal(signal.SIGTERM)(signal.SIGTERM,None)
    monkeypatch.setattr(service,'build_worker',lambda config:(Database(),Transport()))
    monkeypatch.setattr(service,'measure_accelerator',lambda root,backend:Capacity(100,200,300))
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'mps'})
    service.run_worker(config)
    assert calls==[('preflight','mps'),('capacity','mps',100,300,200)]


def test_worker_lens_registry_config_is_paired_and_cpu_only(tmp_path):
    from hashlib import sha256
    from importlib import metadata
    import platform,sys
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.optical.registry import LensRegistry
    from photo_worker.operations.optical.runtime_lock import OpticalRuntimeLock,installed_component_hashes
    env={**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'}
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,'PHOTO_LENS_REGISTRY':'missing.json'})
    runtime=OpticalRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())
    raw=canonical_bytes(LensRegistry(schema_version=1,runtime=runtime,profiles=[]))
    path=tmp_path/'lens-registry.json';path.write_bytes(raw)
    configured={**env,'PHOTO_LENS_REGISTRY':str(path),'PHOTO_LENS_REGISTRY_SHA256':sha256(raw).hexdigest()}
    assert WorkerConfig.from_env(configured).lens_registry.profiles==[]
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**configured,'PHOTO_WORKER_CLASS':'media'})
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**configured,'PHOTO_LENS_REGISTRY_SHA256':'a'*64})
