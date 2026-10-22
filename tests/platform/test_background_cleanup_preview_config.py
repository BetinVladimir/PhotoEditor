"""Startup boundaries only; injected providers are not hardware qualification."""
from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.runtime import background_cleanup_preview_config as module
from photo_worker.runtime.service import WorkerConfigurationError


def test_cleanup_preview_disabled_has_no_configuration_dependencies():
    assert module.background_cleanup_preview_from_environment(SimpleNamespace(),lambda key,default='':default) is None


@pytest.mark.parametrize('enabled',['','TRUE','1','yes'])
def test_cleanup_preview_requires_explicit_boolean_enablement(enabled):
    with pytest.raises(WorkerConfigurationError,match='enablement'):
        module.background_cleanup_preview_from_environment(SimpleNamespace(),lambda key,default='':enabled)


@pytest.mark.parametrize('resource,backend',[('cpu','cpu'),('gpu','metal'),('gpu','unknown')])
def test_cleanup_preview_refuses_mismatched_worker_before_dependencies(resource,backend):
    env={'PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED':'true'}
    with pytest.raises(WorkerConfigurationError,match='matching GPU-class worker'):
        module.background_cleanup_preview_from_environment(SimpleNamespace(resource_class=resource,backend=backend),
            lambda key,default='':env.get(key,default))


@pytest.mark.parametrize('backend,missing_root',[('cuda',False),('mps',False),('cpu',False),('mps',True)])
def test_cleanup_preview_factory_receives_own_backend_and_pinned_presentation(tmp_path,monkeypatch,backend,missing_root):
    from photo_worker.operations.semantic import exposure_color
    root=tmp_path/'missing' if missing_root else tmp_path
    lock=b'{"schema_version":1}'
    if not missing_root:(root/'profiles.lock.json').write_bytes(lock)
    registry=SimpleNamespace(resolve=lambda name:SimpleNamespace(sha256='b'*64))
    monkeypatch.setattr(module,'ICCRegistry',lambda path:registry)
    monkeypatch.setattr(exposure_color,'local_exposure_icc_verifier',lambda registry:None)
    monkeypatch.setattr(module,'TaskDatabase',lambda url:('database',url))
    clients=[];calls=[]
    def client(*args,**kwargs):
        clients.append((args,kwargs))
        return SimpleNamespace()
    import minio
    monkeypatch.setattr(minio,'Minio',client)
    installation=object()
    def factory(**kwargs):
        calls.append(kwargs)
        return installation
    monkeypatch.setattr(module,'sql_background_cleanup_preview_factory',factory)
    env={'PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED':'true','S3_ENDPOINT':'https://objects.example.test',
        'S3_BUCKET':'photos','S3_ACCESS_KEY':'test-access','S3_SECRET_KEY':'test-secret',
        **{'PHOTO_BACKGROUND_CLEANUP_PREVIEW_'+name.upper():'a'*64 for name in
            ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}}
    config=SimpleNamespace(resource_class='gpu',backend=backend,icc_root=root,development=False,database_url='postgres://unused')
    def run():return module.background_cleanup_preview_from_environment(config,lambda key,default='':env.get(key,default))
    if missing_root:
        with pytest.raises(WorkerConfigurationError,match='presentation pins'):run()
        assert calls==[]
    else:
        assert run() is installation and len(calls)==1
        assert calls[0]['backend']==backend and calls[0]['icc_registry'] is registry
        assert calls[0]['presentation'].icc_lock_sha==sha256(lock).hexdigest()
        assert calls[0]['bucket']=='photos'
        assert calls[0]['model_lock_sha']==calls[0]['runtime_lock_sha']==calls[0]['device_profile_sha']==calls[0]['dataset_sha']=='a'*64
    assert len(clients)==(0 if missing_root else 1)
    if clients:assert clients[0][1]['http_client'].connection_pool_kw['retries'].total is False


@pytest.mark.parametrize('field',['model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'])
@pytest.mark.parametrize('invalid',['','a'*63,'G'*64])
def test_cleanup_preview_rejects_missing_or_invalid_measurements_before_icc(monkeypatch,field,invalid):
    env={'PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED':'true',
        **{'PHOTO_BACKGROUND_CLEANUP_PREVIEW_'+name.upper():'a'*64 for name in
            ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}}
    env['PHOTO_BACKGROUND_CLEANUP_PREVIEW_'+field.upper()]=invalid
    def unexpected(*args,**kwargs):raise AssertionError('dependency accessed before validation')
    monkeypatch.setattr(module,'ICCRegistry',unexpected)
    monkeypatch.setattr(module,'TaskDatabase',unexpected)
    config=SimpleNamespace(resource_class='gpu',backend='mps',icc_root='/unused')
    with pytest.raises(WorkerConfigurationError,match='measured runtime hashes'):
        module.background_cleanup_preview_from_environment(config,lambda key,default='':env.get(key,default))


@pytest.mark.parametrize('key,value',[
    ('S3_ENDPOINT','http://objects.example.test'),
    ('S3_ENDPOINT','https://user:secret@objects.example.test'),
    ('S3_ENDPOINT','https://objects.example.test/path'),
    ('S3_ENDPOINT','https://objects.example.test?query=1'),
    ('S3_ENDPOINT','https://objects.example.test#fragment'),
    ('S3_ENDPOINT','https://objects.example.test:invalid'),
    ('S3_BUCKET','photos..private'),('S3_BUCKET','Photos'),
    ('S3_ACCESS_KEY',''),('S3_SECRET_KEY',''),
])
def test_cleanup_preview_rejects_invalid_storage_before_client_or_database(monkeypatch,key,value):
    from photo_worker.operations.semantic import exposure_color
    import minio
    def unexpected(*args,**kwargs):raise AssertionError('storage dependency accessed before validation')
    monkeypatch.setattr(module,'ICCRegistry',lambda path:object())
    monkeypatch.setattr(exposure_color,'local_exposure_icc_verifier',lambda registry:None)
    monkeypatch.setattr(minio,'Minio',unexpected)
    monkeypatch.setattr(module,'TaskDatabase',unexpected)
    env={'PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED':'true','S3_ENDPOINT':'https://objects.example.test',
        'S3_BUCKET':'photos','S3_ACCESS_KEY':'test-access','S3_SECRET_KEY':'test-secret',
        **{'PHOTO_BACKGROUND_CLEANUP_PREVIEW_'+name.upper():'a'*64 for name in
            ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}}
    env[key]=value
    config=SimpleNamespace(resource_class='gpu',backend='mps',icc_root='/unused',development=False)
    with pytest.raises(WorkerConfigurationError,match='object storage configuration'):
        module.background_cleanup_preview_from_environment(config,lambda key,default='':env.get(key,default))


@pytest.mark.parametrize('phase',['registry','working_profile'])
@pytest.mark.parametrize('error_type',[OSError,ValueError,TypeError,KeyError])
def test_cleanup_preview_icc_failure_is_configuration_error_before_storage(monkeypatch,phase,error_type):
    from photo_worker.operations.semantic import exposure_color
    import minio
    def fail(*args,**kwargs):raise error_type('invalid profile')
    def unexpected(*args,**kwargs):raise AssertionError('dependency accessed after ICC failure')
    monkeypatch.setattr(module,'ICCRegistry',fail if phase=='registry' else lambda path:object())
    monkeypatch.setattr(exposure_color,'local_exposure_icc_verifier',fail if phase=='working_profile' else unexpected)
    monkeypatch.setattr(minio,'Minio',unexpected)
    monkeypatch.setattr(module,'TaskDatabase',unexpected)
    env={'PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED':'true',
        **{'PHOTO_BACKGROUND_CLEANUP_PREVIEW_'+name.upper():'a'*64 for name in
            ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}}
    with pytest.raises(WorkerConfigurationError,match='ICC configuration'):
        module.background_cleanup_preview_from_environment(SimpleNamespace(resource_class='gpu',backend='mps',icc_root='/unused'),
            lambda key,default='':env.get(key,default))


@pytest.mark.parametrize('mode',['missing_lock','short_lock','oversized_lock','invalid_srgb'])
def test_cleanup_preview_presentation_failure_precedes_storage_and_database(tmp_path,monkeypatch,mode):
    from photo_worker.operations.semantic import exposure_color
    import minio
    if mode!='missing_lock':
        (tmp_path/'profiles.lock.json').write_bytes(b'x' if mode=='short_lock' else b'x'*65537 if mode=='oversized_lock' else b'{}')
    registry=SimpleNamespace(resolve=lambda name:SimpleNamespace(sha256='bad' if mode=='invalid_srgb' else 'b'*64))
    monkeypatch.setattr(module,'ICCRegistry',lambda path:registry)
    monkeypatch.setattr(exposure_color,'local_exposure_icc_verifier',lambda registry:None)
    def unexpected(*args,**kwargs):raise AssertionError('dependency accessed after presentation failure')
    monkeypatch.setattr(minio,'Minio',unexpected)
    monkeypatch.setattr(module,'TaskDatabase',unexpected)
    monkeypatch.setattr(module,'sql_background_cleanup_preview_factory',unexpected)
    env={'PHOTO_BACKGROUND_CLEANUP_PREVIEW_ENABLED':'true','S3_ENDPOINT':'https://objects.example.test',
        'S3_BUCKET':'photos','S3_ACCESS_KEY':'test-access','S3_SECRET_KEY':'test-secret',
        **{'PHOTO_BACKGROUND_CLEANUP_PREVIEW_'+name.upper():'a'*64 for name in
            ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}}
    config=SimpleNamespace(resource_class='gpu',backend='mps',icc_root=tmp_path,development=False)
    with pytest.raises(WorkerConfigurationError,match='presentation pins'):
        module.background_cleanup_preview_from_environment(config,lambda key,default='':env.get(key,default))
