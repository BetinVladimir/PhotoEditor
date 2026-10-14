"""Identity tasks require measured process-owned runtime pins."""
from types import SimpleNamespace
from uuid import UUID

import pytest

from photo_worker.operations.identity.authority import identity_runtime_authority
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.identity_config import identity_runtime_locks_from_env


def _expected():
    return dict(model_lock_sha='a' * 64, runtime_lock_sha='b' * 64,
                device_profile_sha='c' * 64, backend='cpu')


def _context():
    profile = dict(_expected(), dataset_sha='d' * 64,
        qualification_policy_id=str(UUID(int=1)), qualification_id=str(UUID(int=2)),
        qualification_report={'artifact_id': str(UUID(int=3)), 'sha256': 'e' * 64},
        calibration={'artifact_id': str(UUID(int=4)), 'sha256': 'f' * 64})
    return SimpleNamespace(lease=SimpleNamespace(execution_profile=profile),
                           check_active=lambda: None)


def test_identity_runtime_authority_rechecks_profile_and_measurement():
    expected = _expected()
    current = dict(expected)
    verify = identity_runtime_authority(lambda: dict(current), **expected)
    ctx = _context()
    assert verify(ctx) is True
    current['runtime_lock_sha'] = '0' * 64
    with pytest.raises(OperationFailure, match='IDENTITY_RUNTIME_UNQUALIFIED'):
        verify(ctx)
    current.update(expected)
    ctx.lease.execution_profile['dataset_sha'] = 'bad'
    with pytest.raises(OperationFailure, match='IDENTITY_RUNTIME_UNQUALIFIED'):
        verify(ctx)


def test_identity_runtime_config_requires_complete_cpu_or_cuda_locks():
    expected = _expected()
    env = {'PHOTO_IDENTITY_RUNTIME_ENABLED': 'true',
           'PHOTO_IDENTITY_MODEL_LOCK_SHA': expected['model_lock_sha'],
           'PHOTO_IDENTITY_RUNTIME_LOCK_SHA': expected['runtime_lock_sha'],
           'PHOTO_IDENTITY_DEVICE_PROFILE_SHA': expected['device_profile_sha']}
    assert identity_runtime_locks_from_env(env, 'gpu', 'cpu', True) == (
        expected['model_lock_sha'], expected['runtime_lock_sha'],
        expected['device_profile_sha'])
    from photo_worker.runtime.service import WorkerConfigurationError
    with pytest.raises(WorkerConfigurationError):
        identity_runtime_locks_from_env(env, 'gpu', 'mps', True)
    with pytest.raises(WorkerConfigurationError):
        identity_runtime_locks_from_env(env, 'gpu', 'cpu', False)
    with pytest.raises(WorkerConfigurationError):
        identity_runtime_locks_from_env({'PHOTO_IDENTITY_MODEL_LOCK_SHA': 'a' * 64}, 'gpu', 'cpu', True)
