"""Qualified reflection admission never substitutes a cached model for live SQL."""

from contextlib import nullcontext
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from photo_worker.models.pool import ModelPool
from photo_worker.operations.image.reflection_admission import ReflectionModelAdmission
from photo_worker.runtime.executor import OperationFailure


def installation(*, status='passed', lease=True, qualification=True):
    pool = object.__new__(ModelPool)
    pool.lock = SimpleNamespace(lock_id='a' * 64, models=[SimpleNamespace(
        id='reflection_rgb', approved_for_install=True,
        backends={'cpu': SimpleNamespace(status=status)})])
    pool.acquire = Mock(return_value=nullcontext(SimpleNamespace(
        size=128, infer=lambda *_args, **_kwargs: None)))
    db = SimpleNamespace(restoration_lease_ready=Mock(return_value=lease),
                         restoration_qualification_ready=Mock(return_value=qualification))
    return ReflectionModelAdmission(db, pool, 'cpu', 'c' * 64), db, pool


def context():
    command = SimpleNamespace(operation='image.reflection', operation_version='1',
                              params=SimpleNamespace(sha256='b' * 64))
    return SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'backend': 'cpu', 'restoration_runtime_sha': 'c' * 64}),
        check_active=lambda: None)


def test_reflection_installation_requires_qualified_model():
    with pytest.raises(ValueError, match='REFLECTION_MODEL_UNQUALIFIED'):
        installation(status='untested')


def test_reflection_admission_refuses_without_live_sql_or_exact_lock(monkeypatch):
    from photo_worker.operations.image import reflection_admission
    monkeypatch.setattr(reflection_admission, 'canonical_bytes', lambda _: b'{}')
    monkeypatch.setattr(reflection_admission, 'canonical_hash', lambda _: 'b' * 64)
    admission, db, pool = installation(qualification=False)
    params = SimpleNamespace(model_lock_sha='a' * 64)
    calibration = SimpleNamespace(model_lock_sha='a' * 64)
    ctx = context()
    assert admission.admit(ctx, params, None, None, None, calibration) is False
    assert db.restoration_lease_ready.call_count == 1
    assert db.restoration_qualification_ready.call_count == 1
    db.restoration_qualification_ready.return_value = True
    assert admission.admit(ctx, params, None, None, None, calibration) is True
    assert db.restoration_lease_ready.call_count == 2
    params.model_lock_sha = 'c' * 64
    assert admission.admit(ctx, params, None, None, None, calibration) is False
    assert db.restoration_lease_ready.call_count == 2
    pool.acquire.assert_not_called()


def test_reflection_engine_lease_uses_exact_qualified_pool():
    admission, _, pool = installation()
    ctx = context()
    with admission.acquire(ctx, 'cpu') as engine:
        assert engine.size == 128
    pool.acquire.assert_called_once_with('reflection_rgb', 'cpu')
    with pytest.raises(OperationFailure, match='BACKEND_UNAVAILABLE'):
        with admission.acquire(ctx, 'cuda'):
            pass
