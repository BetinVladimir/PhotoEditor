from pathlib import Path
from types import SimpleNamespace
import pytest
from photo_worker.runtime.exposure_config import local_exposure_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def config_env():
    config=SimpleNamespace(resource_class='cpu',backend='cpu',development=False,
        database_url='postgres://example',icc_root=Path(__file__).resolve().parents[2]/'resources/icc')
    env=dict(PHOTO_LOCAL_EXPOSURE_ENABLED='true',S3_ENDPOINT='https://objects.test',
        S3_BUCKET='owned',S3_ACCESS_KEY='access-test',S3_SECRET_KEY='secret-test')
    for name in ('MODEL_LOCK_SHA','RUNTIME_LOCK_SHA','DEVICE_PROFILE_SHA','DATASET_SHA'):
        env['PHOTO_LOCAL_EXPOSURE_'+name]='a'*64
    return config,env


def test_disabled_has_no_dependency_io():
    assert local_exposure_from_environment(object(),{}.get) is None


@pytest.mark.parametrize('patch',[{'PHOTO_LOCAL_EXPOSURE_ENABLED':'yes'},
    {'PHOTO_LOCAL_EXPOSURE_RUNTIME_LOCK_SHA':''},{'S3_ENDPOINT':'http://objects.test'},
    {'S3_ENDPOINT':'https://user:secret@objects.test'},{'S3_ENDPOINT':'https://objects.test/path'},
    {'S3_BUCKET':'bad/bucket'},{'S3_SECRET_KEY':''}])
def test_invalid_configuration_denied_without_secret_echo(patch):
    config,env=config_env();env.update(patch)
    with pytest.raises(WorkerConfigurationError) as error:
        local_exposure_from_environment(config,env.get)
    assert 'secret-test' not in str(error.value) and 'access-test' not in str(error.value)


def test_concrete_factory_startup_does_not_activate_operations():
    config,env=config_env()
    assert callable(local_exposure_from_environment(config,env.get))
    config.resource_class='gpu'
    with pytest.raises(WorkerConfigurationError,match='CPU'):
        local_exposure_from_environment(config,env.get)
    config.resource_class='cpu';config.icc_root=None
    with pytest.raises(WorkerConfigurationError,match='ICC'):
        local_exposure_from_environment(config,env.get)


def test_cli_composes_assembly_and_exposure(monkeypatch):
    from typer.testing import CliRunner
    from photo_worker.cli import app
    import photo_worker.runtime.service as service
    import photo_worker.runtime.exposure_config as exposure
    import photo_worker.runtime.semantic_assembly_config as assembly
    config=object();one=object();two=object();calls=[]
    monkeypatch.setattr(service.WorkerConfig,'from_env',classmethod(lambda cls:config))
    monkeypatch.setattr(exposure,'local_exposure_from_environment',lambda value:one)
    monkeypatch.setattr(assembly,'semantic_assembly_from_environment',lambda value:two)
    monkeypatch.setattr(service,'run_worker',lambda value,**kwargs:calls.append((value,kwargs)))
    result=CliRunner().invoke(app,['run'])
    assert result.exit_code==0
    assert calls==[(config,dict(local_exposure_installation_factory=one,
        semantic_assembly_installation_factory=two))]
