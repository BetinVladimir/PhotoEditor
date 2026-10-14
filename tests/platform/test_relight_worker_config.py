from types import SimpleNamespace
import pytest
from photo_worker.runtime import relight_config as module


@pytest.mark.parametrize('mode',['disabled','invalid','gpu','pin','lock','ok'])
def test_explicit_worker_runtime_precedes_object_storage(monkeypatch,tmp_path,mode):
    from photo_worker.runtime import donor_config
    calls=[]
    config=SimpleNamespace(resource_class='gpu' if mode=='gpu' else 'cpu',backend='cpu')
    env=dict(PHOTO_RELIGHT_ENABLED='true',PHOTO_RELIGHT_RUNTIME_LOCK=str(tmp_path/'runtime.json'),PHOTO_RELIGHT_RUNTIME_SHA='a'*64)
    if mode=='disabled':env={}
    if mode=='invalid':env['PHOTO_RELIGHT_ENABLED']='yes'
    if mode=='pin':env['PHOTO_RELIGHT_RUNTIME_SHA']='invalid'
    def runtime(*args):
        calls.append('runtime')
        if mode=='lock':raise ValueError('bad lock')
        return {'runtime':'fixture'}
    monkeypatch.setattr(module,'load_relight_runtime_lock',runtime)
    monkeypatch.setattr(donor_config,'qualified_object_storage_from_environment',lambda *a:calls.append('objects') or (object(),'bucket'))
    if mode=='disabled':assert module.relight_installation_from_environment(config,env.get) is None and calls==[]
    elif mode=='ok':
        result=module.relight_installation_from_environment(config,env.get)
        assert result.bucket=='bucket' and calls==['runtime','objects']
    else:
        with pytest.raises(ValueError):module.relight_installation_from_environment(config,env.get)
        assert 'objects' not in calls


def test_build_worker_rejects_untyped_relight_before_tls(monkeypatch):
    from photo_worker.runtime import service
    monkeypatch.setattr(service,'client_tls',lambda *a:pytest.fail('TLS before validation'))
    config=SimpleNamespace(resource_class='cpu',backend='cpu',person_runtime_locks=None)
    with pytest.raises(ValueError,match='Invalid relight'):
        service.build_worker(config,relight_installation=object())


def test_cpu_build_installs_qualified_relight_operation(monkeypatch,tmp_path):
    import ssl,sys,platform
    from importlib import metadata
    from photo_worker.runtime import service
    from photo_worker.operations.portrait.relight_runtime_lock import RelightRuntimeLock,installed_component_hashes
    from photo_worker.contracts.base import canonical_hash
    from test_worker_service import environment
    config=service.WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    lock=RelightRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),
        implementation=platform.python_implementation(),machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785','opencv-python-headless')},
        components=installed_component_hashes())
    installation=module.RelightInstallation(SimpleNamespace(get_object=lambda *a:None),'bucket',lock,canonical_hash(lock))
    monkeypatch.setattr(service,'client_tls',lambda *a:ssl.create_default_context())
    _,baseline=service.build_worker(config)
    assert 'image.relight' not in baseline.executor.operations
    _,installed=service.build_worker(config,relight_installation=installation)
    assert installed.executor.operations['image.relight'].resource_class=='cpu'


def test_run_worker_forwards_relight_installation(monkeypatch):
    from photo_worker.runtime import service
    marker=object();calls=[]
    def build(config,**kwargs):
        calls.append(kwargs);raise RuntimeError('stop before network')
    monkeypatch.setattr(service,'build_worker',build)
    with pytest.raises(RuntimeError,match='stop before network'):
        service.run_worker(None,relight_installation=marker)
    assert calls==[{'relight_installation':marker}]
