from types import SimpleNamespace
import pytest
from photo_worker.runtime.donor_config import donor_options_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def test_disabled_donor_does_not_load_any_runtime_files():
    assert donor_options_from_environment(None,lambda key,default=None:default) is None


@pytest.mark.parametrize('value',['TRUE','1','yes'])
def test_donor_config_rejects_ambiguous_enablement(value):
    with pytest.raises(WorkerConfigurationError):donor_options_from_environment(None,lambda key,default=None:value)


def test_enabled_donor_requires_cpu_before_file_access():
    with pytest.raises(WorkerConfigurationError,match='CPU'):
        donor_options_from_environment(SimpleNamespace(resource_class='gpu',backend='cuda'),lambda key,default=None:'true' if key=='PHOTO_DONOR_ENABLED' else default)


def test_disabled_installation_does_not_construct_object_client():
    from photo_worker.runtime.donor_config import donor_installation_from_environment
    assert donor_installation_from_environment(None,lambda key,default=None:default) is None


def test_run_cli_installs_donor_handler_on_cpu_worker(monkeypatch):
    from typer.testing import CliRunner
    from photo_worker.cli import app
    import photo_worker.runtime.service as service
    import photo_worker.runtime.donor_config as donor_config
    config=SimpleNamespace(resource_class='cpu',backend='cpu')
    installation=object();calls=[]
    monkeypatch.setattr(service.WorkerConfig,'from_env',classmethod(lambda cls:config))
    monkeypatch.setattr(donor_config,'donor_installation_from_environment',
        lambda value:installation if value is config else None)
    monkeypatch.setattr(service,'run_worker',lambda value,**kwargs:calls.append((value,kwargs)))
    result=CliRunner().invoke(app,['run'])
    assert result.exit_code==0
    assert len(calls)==1 and calls[0][0] is config
    assert calls[0][1]['donor_installation'] is installation


@pytest.mark.parametrize('endpoint',['http://example.com','https://user:secret@example.com','https://example.com/path','https://example.com?key=secret'])
def test_donor_storage_rejects_unsafe_endpoint_before_client(monkeypatch,endpoint):
    from photo_worker.runtime import donor_config as module
    monkeypatch.setattr(module,'donor_options_from_environment',lambda *args:object())
    values={'S3_ENDPOINT':endpoint,'S3_BUCKET':'bucket','S3_ACCESS_KEY':'access','S3_SECRET_KEY':'secret'}
    with pytest.raises(WorkerConfigurationError,match='object storage'):
        module.donor_installation_from_environment(SimpleNamespace(development=False),lambda key,default=None:values.get(key,default))


def test_donor_storage_constructs_bounded_tls_client_without_io(monkeypatch):
    from photo_worker.runtime import donor_config as module
    import minio
    options=object();calls=[]
    monkeypatch.setattr(module,'donor_options_from_environment',lambda *args:options)
    def client(endpoint,**kwargs):
        assert endpoint=='example.com:9000' and kwargs['secure'] is True
        assert kwargs['http_client'].connection_pool_kw['retries'].total is False
        calls.append('client');return object()
    monkeypatch.setattr(minio,'Minio',client)
    values={'S3_ENDPOINT':'https://example.com:9000','S3_BUCKET':'bucket','S3_ACCESS_KEY':'access','S3_SECRET_KEY':'secret'}
    installation=module.donor_installation_from_environment(SimpleNamespace(development=False),lambda key,default=None:values.get(key,default))
    assert installation.options is options and installation.bucket=='bucket' and calls==['client']


@pytest.mark.parametrize('total',[True,0,-1,2**53])
def test_donor_total_scratch_must_be_positive_exact_integer(total):
    config=SimpleNamespace(resource_class='cpu',backend='cpu',max_scratch_bytes=total)
    with pytest.raises(WorkerConfigurationError,match='total scratch'):
        donor_options_from_environment(config,lambda key,default=None:'true' if key=='PHOTO_DONOR_ENABLED' else default)


@pytest.mark.parametrize('material,accepted',[(50,True),(51,False)])
def test_donor_combined_mapping_budget_checked_before_runtime_io(tmp_path,monkeypatch,material,accepted):
    from photo_worker.runtime import donor_config as module
    calls=[];lock=object()
    def load(*args):calls.append('lock');return lock
    monkeypatch.setattr(module,'load_donor_portrait_runtime_lock',load)
    values={'PHOTO_DONOR_ENABLED':'true','PHOTO_DONOR_RUNTIME_SHA':'a'*64,
        'PHOTO_DONOR_RUNTIME_LOCK':str(tmp_path/'unused.json'),
        'PHOTO_DONOR_MASK_SCRATCH_ROOT':str(tmp_path),'PHOTO_DONOR_MATERIAL_SCRATCH_ROOT':str(tmp_path),
        'PHOTO_DONOR_MASK_MAX_BYTES':'50','PHOTO_DONOR_MATERIAL_MAX_BYTES':str(material)}
    config=SimpleNamespace(resource_class='cpu',backend='cpu',max_scratch_bytes=100)
    def run():return module.donor_options_from_environment(config,lambda key,default=None:values.get(key,default))
    if accepted:
        assert run().runtime_lock is lock and calls==['lock']
    else:
        with pytest.raises(WorkerConfigurationError,match='aggregate'):run()
        assert calls==[]
