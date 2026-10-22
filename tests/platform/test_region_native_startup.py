"""Authored startup refusal scenarios; execution deferred."""
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.region_config import region_installation_from_env
from photo_worker.runtime.service import WorkerConfigurationError


def installation(backend='mps'):
    return dict(qualification=dict(runtime_lock_sha='a'*64,model_lock_sha='b'*64,
        icc_lock_sha='c'*64,device_profile_sha='d'*64,dataset_sha='e'*64,
        policy_sha='f'*64,backend=backend),runtime_path='/installed/region.json',
        calibration_path='/installed/calibration.json',calibration_sha='1'*64)


def env(tmp_path,document):
    raw=canonical_bytes(document);path=tmp_path/'region.json';path.write_bytes(raw)
    return dict(PHOTO_REGION_RUNTIME_ENABLED='true',PHOTO_REGION_INSTALLATION_FILE=str(path),
        PHOTO_REGION_INSTALLATION_SHA=sha256(raw).hexdigest())


@pytest.mark.parametrize('backend',['mps','cuda'])
def test_native_startup_selects_exact_pinned_backend(tmp_path,backend):
    selected=region_installation_from_env(env(tmp_path,installation(backend)),'gpu',backend)
    assert selected.qualification.backend==backend


@pytest.mark.parametrize('resource,backend',[('cpu','mps'),('gpu','cpu'),('gpu','cuda')])
def test_native_startup_refuses_wrong_routing(tmp_path,resource,backend):
    with pytest.raises(WorkerConfigurationError):
        region_installation_from_env(env(tmp_path,installation()),resource,backend)


def test_native_startup_refuses_corrupted_installation(tmp_path):
    values=env(tmp_path,installation());values['PHOTO_REGION_INSTALLATION_SHA']='0'*64
    with pytest.raises(WorkerConfigurationError):region_installation_from_env(values,'gpu','mps')


def test_native_startup_disabled_never_consumes_stale_pins(tmp_path):
    assert region_installation_from_env({},'gpu','mps') is None
    values=env(tmp_path,installation());values['PHOTO_REGION_RUNTIME_ENABLED']='false'
    with pytest.raises(WorkerConfigurationError):region_installation_from_env(values,'gpu','mps')


def test_native_startup_rejects_unknown_callback_configuration(tmp_path):
    value=installation();value['factory']='arbitrary.module:factory'
    with pytest.raises(WorkerConfigurationError):
        region_installation_from_env(env(tmp_path,value),'gpu','mps')
