from types import SimpleNamespace
import pytest
from photo_worker.runtime.semantic_assembly_config import semantic_assembly_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def test_disabled_assembly_has_no_dependency_io():
    assert semantic_assembly_from_environment(object(),{}.get) is None


@pytest.mark.parametrize('patch',[{'PHOTO_SEMANTIC_ASSEMBLY_ENABLED':'yes'},{'S3_ENDPOINT':'http://objects.test'},{'S3_ENDPOINT':'https://user:secret@objects.test'},{'S3_ENDPOINT':'https://objects.test/path'},{'S3_ENDPOINT':'https://objects.test?secret=x'},{'S3_BUCKET':'bad/bucket'},{'S3_SECRET_KEY':''}])
def test_configuration_denied_without_secret_echo(patch):
    config=SimpleNamespace(resource_class='cpu',backend='cpu',development=False,database_url='postgres://example')
    env=dict(PHOTO_SEMANTIC_ASSEMBLY_ENABLED='true',S3_ENDPOINT='https://objects.test',S3_BUCKET='owned',S3_ACCESS_KEY='access-test',S3_SECRET_KEY='secret-test')
    env.update(patch)
    with pytest.raises(WorkerConfigurationError) as error:semantic_assembly_from_environment(config,env.get)
    assert 'secret-test' not in str(error.value) and 'access-test' not in str(error.value)


@pytest.mark.parametrize('development',[False,True])
def test_factory_configures_exact_store_without_starting_worker(monkeypatch,development):
    import minio
    import photo_worker.runtime.semantic_assembly_config as module
    calls=[];client=object();database=object();factory=object()
    def construct(endpoint,**kwargs):
        policy=kwargs['http_client'].connection_pool_kw
        assert policy['timeout'].connect_timeout==5 and policy['timeout'].read_timeout==10
        assert policy['retries'].total is False and policy['cert_reqs']=='CERT_REQUIRED'
        assert policy['ca_certs']
        calls.append((endpoint,kwargs['secure']));return client
    monkeypatch.setattr(minio,'Minio',construct)
    monkeypatch.setattr(module,'TaskDatabase',lambda dsn:database)
    def install(**kwargs):
        assert kwargs['database'] is database and kwargs['objects']._client is client and kwargs['bucket']=='owned'
        return factory
    monkeypatch.setattr(module,'sql_semantic_assembly_factory',install)
    config=SimpleNamespace(resource_class='cpu',backend='cpu',development=development,database_url='postgres://example')
    env=dict(PHOTO_SEMANTIC_ASSEMBLY_ENABLED='true',S3_ENDPOINT=('http' if development else 'https')+'://objects.test:9000',S3_BUCKET='owned',S3_ACCESS_KEY='access-test',S3_SECRET_KEY='secret-test')
    assert semantic_assembly_from_environment(config,env.get) is factory
    assert calls==[('objects.test:9000',not development)]
    config.resource_class='gpu'
    with pytest.raises(WorkerConfigurationError,match='CPU'):semantic_assembly_from_environment(config,env.get)


def test_run_cli_forwards_assembly_installation(monkeypatch):
    from typer.testing import CliRunner
    from photo_worker.cli import app
    import photo_worker.runtime.service as service
    import photo_worker.runtime.semantic_assembly_config as module
    config=object();factory=object();calls=[]
    monkeypatch.setattr(service.WorkerConfig,'from_env',classmethod(lambda cls:config))
    monkeypatch.setattr(module,'semantic_assembly_from_environment',lambda value:factory if value is config else None)
    monkeypatch.setattr(service,'run_worker',lambda value,**kwargs:calls.append((value,kwargs)))
    result=CliRunner().invoke(app,['run'])
    assert result.exit_code==0
    assert calls==[(config,dict(semantic_assembly_installation_factory=factory))]
