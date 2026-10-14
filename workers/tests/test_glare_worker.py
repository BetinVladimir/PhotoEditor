"""V28 glare handler refuses an unconfigured authority before artifact reads."""

from types import SimpleNamespace

import pytest

from photo_worker.operations.portrait.glare import execute
from photo_worker.runtime.executor import OperationFailure


def context(operation='portrait.glare'):
    command = SimpleNamespace(operation=operation, operation_version='1',
                              resource_class='gpu')
    lease = SimpleNamespace(command=command, execution_profile={
        'backend': 'cpu', 'renderer_version': 'v2.2'})
    return SimpleNamespace(lease=lease, check_active=lambda: None)


def test_glare_worker_denies_missing_live_authority():
    with pytest.raises(OperationFailure, match='GLARE_AUTHORITY_UNAVAILABLE'):
        execute(context(), object(), None)


def test_glare_worker_denies_wrong_operation_before_artifact_io():
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        execute(context('portrait.open_eyes'), object(),
                SimpleNamespace(admit=lambda *a: True, reader=lambda *a: None))
