"""CPU copy startup boundaries; no physical ICC/qualification proof."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime import shown_background_cleanup_config as module
from photo_worker.runtime.service import WorkerConfigurationError


def test_shown_cleanup_disabled_requires_no_dependencies():
    assert module.shown_background_cleanup_from_environment(SimpleNamespace(),lambda key,default='':default) is None


@pytest.mark.parametrize('enabled',['','TRUE','1','yes'])
def test_shown_cleanup_invalid_enablement(enabled):
    with pytest.raises(WorkerConfigurationError,match='enablement'):
        module.shown_background_cleanup_from_environment(SimpleNamespace(),lambda key,default='':enabled)


@pytest.mark.parametrize('resource,backend',[('gpu','cpu'),('cpu','cuda'),('cpu','mps')])
def test_shown_cleanup_requires_exact_cpu_lane(resource,backend):
    with pytest.raises(WorkerConfigurationError,match='requires CPU worker'):
        module.shown_background_cleanup_from_environment(SimpleNamespace(resource_class=resource,backend=backend),
            lambda key,default='':'true' if key=='PHOTO_SHOWN_BACKGROUND_CLEANUP_ENABLED' else default)


def environment():
    return {'PHOTO_SHOWN_BACKGROUND_CLEANUP_ENABLED':'true','S3_ENDPOINT':'https://objects.example.test',
        'S3_BUCKET':'photos','S3_ACCESS_KEY':'test-access','S3_SECRET_KEY':'test-secret',
        **{'PHOTO_SHOWN_BACKGROUND_CLEANUP_'+name.upper():'a'*64 for name in
            ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}}


@pytest.mark.parametrize('field',['model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'])
def test_shown_cleanup_rejects_missing_measurement_before_icc(monkeypatch,field):
    env=environment();env.pop('PHOTO_SHOWN_BACKGROUND_CLEANUP_'+field.upper())
    def unexpected(*args,**kwargs):raise AssertionError('ICC accessed before validation')
    monkeypatch.setattr(module,'ICCRegistry',unexpected)
    with pytest.raises(WorkerConfigurationError,match='measured runtime hashes'):
        module.shown_background_cleanup_from_environment(SimpleNamespace(resource_class='cpu',backend='cpu',icc_root='/unused'),
            lambda key,default='':env.get(key,default))


@pytest.mark.parametrize('mode',['positive','icc_failure','storage_failure'])
def test_shown_cleanup_startup_hands_own_pins_to_factory(monkeypatch,mode):
    from photo_worker.operations.semantic import exposure_color
    import minio
    env=environment();calls=[];clients=[];database=[];registry=object();installation=object()
    monkeypatch.setattr(module,'ICCRegistry',lambda path:registry)
    def verify(value):
        assert value is registry
        if mode=='icc_failure':raise ValueError('invalid working ICC')
    monkeypatch.setattr(exposure_color,'local_exposure_icc_verifier',verify)
    monkeypatch.setattr(module,'TaskDatabase',lambda url:database.append(url) or object())
    monkeypatch.setattr(minio,'Minio',lambda *args,**kwargs:clients.append((args,kwargs)) or SimpleNamespace())
    monkeypatch.setattr(module,'sql_shown_background_cleanup_factory',lambda **kwargs:calls.append(kwargs) or installation)
    if mode=='storage_failure':env['S3_ENDPOINT']='http://objects.example.test'
    config=SimpleNamespace(resource_class='cpu',backend='cpu',icc_root='/unused',development=False,database_url='postgres://unused')
    def run():return module.shown_background_cleanup_from_environment(config,lambda key,default='':env.get(key,default))
    if mode!='positive':
        with pytest.raises(WorkerConfigurationError,match='ICC configuration' if mode=='icc_failure' else 'object storage configuration'):run()
        assert not calls and not clients and not database
    else:
        assert run() is installation and len(calls)==1 and len(clients)==1
        assert database==['postgres://unused']
        assert calls[0]['icc_registry'] is registry and calls[0]['bucket']=='photos'
        for field in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'):assert calls[0][field]=='a'*64
        assert clients[0][1]['secure'] is True
        assert clients[0][1]['http_client'].connection_pool_kw['retries'].total is False
