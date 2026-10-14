"""The V28 worker never processes an unqualified reflection model."""

from types import SimpleNamespace

import pytest

from photo_worker.operations.image.reflection import (
    _required_reflection_ram, execute, require_reflection_admission,
)
from photo_worker.runtime.executor import OperationFailure


def context(operation='image.reflection', backend='cpu'):
    command = SimpleNamespace(operation=operation, operation_version='1',
                              resource_class='gpu')
    lease = SimpleNamespace(command=command, execution_profile={
        'backend': backend, 'renderer_version': 'v2.2',
        'resource_estimate': {'ram_bytes': 4 * 1024**3}})
    return SimpleNamespace(lease=lease, check_active=lambda: None)


def test_reflection_worker_requires_installed_admission_before_artifact_io():
    with pytest.raises(OperationFailure, match='REFLECTION_MODEL_UNAVAILABLE'):
        execute(context(), object(), None)


def test_reflection_worker_rejects_wrong_operation_before_artifact_io():
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        execute(context(operation='image.inpaint'), object(),
                SimpleNamespace(lock_sha='a' * 64))


@pytest.mark.parametrize('decision', [None, False, 1, 'authorized', object()])
def test_reflection_admission_requires_explicit_true(decision):
    with pytest.raises(OperationFailure, match='REFLECTION_MODEL_UNAVAILABLE'):
        require_reflection_admission(decision)


def test_reflection_admission_accepts_explicit_true():
    require_reflection_admission(True)


def test_reflection_ram_accounts_for_reflect_padding_before_model_io():
    normal = _required_reflection_ram(1024, 1024, 256)
    narrow = _required_reflection_ram(1, 250_000, 256)
    assert normal == 2 * 1024**3 + 64 * 1024 * 1024
    assert narrow > 4 * 1024**3
    with pytest.raises(OperationFailure, match='RESOURCE_OOM'):
        _required_reflection_ram(1, 1_000_000, 256)
    with pytest.raises(ValueError, match='MODEL_CONTRACT_ERROR'):
        _required_reflection_ram(1024, 1024, 128)
