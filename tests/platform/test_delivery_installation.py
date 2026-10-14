"""Production configuration and SQL admission wiring; no live DB in these units."""
from types import SimpleNamespace
from uuid import uuid4
from photo_worker.contracts.delivery import V2DeliveryPrepareParams
import pytest
from photo_worker.operations.delivery.installation import (
    delivery_enabled_from_env, resolve_delivery_installation,
)
from photo_worker.operations.delivery.adapter import DeliveryInstallation
from photo_worker.operations.delivery.admission import SQLDeliveryAdmission


def test_explicit_cpu_enablement_and_closed_default():
    assert delivery_enabled_from_env({}, 'cpu') is False
    assert delivery_enabled_from_env({'PHOTO_DELIVERY_ENABLED': 'true'}, 'cpu') is True
    for value in ('1', 'TRUE', '', True, None):
        with pytest.raises(ValueError):
            delivery_enabled_from_env({'PHOTO_DELIVERY_ENABLED': value}, 'cpu')
    for pool in ('gpu', 'media', 'archive'):
        with pytest.raises(ValueError):
            delivery_enabled_from_env({'PHOTO_DELIVERY_ENABLED': 'true'}, pool)


def test_installed_admission_uses_same_database_without_source_bypass():
    database = SimpleNamespace(delivery_lease_ready=lambda lease, raw: True,
                               delivery_source_ready=lambda lease, raw: False)
    installation = resolve_delivery_installation(database, True)
    assert isinstance(installation, DeliveryInstallation)
    assert isinstance(installation.admission, SQLDeliveryAdmission)
    assert installation.admission.database is database
    assert installation.admission.verify_source(SimpleNamespace(lease=object()),
        V2DeliveryPrepareParams.model_validate({"schema_version":1,"input":{"artifact_id":str(uuid4()),"sha256":"a"*64}})) is False


def test_disabled_and_conflicting_installations():
    supplied = object()
    assert resolve_delivery_installation(None, False) is None
    assert resolve_delivery_installation(None, False, supplied) is supplied
    with pytest.raises(ValueError, match='CONFLICT'):
        resolve_delivery_installation(None, True, supplied)
    with pytest.raises(ValueError):
        resolve_delivery_installation(None, 1)
    with pytest.raises(ValueError, match='ADMISSION_INVALID'):
        resolve_delivery_installation(object(), True)


def test_worker_environment_persists_delivery_enablement(tmp_path):
    from test_worker_service import environment
    from photo_worker.runtime.service import WorkerConfig, WorkerConfigurationError
    env = {**environment(tmp_path), 'PHOTO_WORKER_CLASS': 'cpu'}
    assert WorkerConfig.from_env(env).delivery_enabled is False
    assert WorkerConfig.from_env({**env, 'PHOTO_DELIVERY_ENABLED': 'true'}).delivery_enabled is True
    with pytest.raises(WorkerConfigurationError):
        WorkerConfig.from_env({**env, 'PHOTO_DELIVERY_ENABLED': 'yes'})
    with pytest.raises(WorkerConfigurationError):
        WorkerConfig.from_env({**env, 'PHOTO_WORKER_CLASS': 'media', 'PHOTO_DELIVERY_ENABLED': 'true'})
