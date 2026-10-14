"""Actual admitted native OOM refusal; injected error, declared model doubles.

Frozen native profiles have no qualified reduction/CPU-fallback declaration.
Do not invent one to make a retry pass: recovery must fail and release resources.
"""
import pytest
from test_v27_target_worker_failure_matrix import lens_objects
from test_v27_target_worker_failure_matrix import test_target_worker_failure_recovery as execute_failure


@pytest.mark.parametrize('operation',('mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose'))
def test_target_worker_oom_refuses_undeclared_fallback(database,lens_objects,monkeypatch,operation):
    for flag in ('PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE','PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST','PHOTO_TEST_V27_TARGET_WORKER_RETRY_SUCCESS_OPERATION','PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE'):
        monkeypatch.delenv(flag,raising=False)
    monkeypatch.setenv('PHOTO_TEST_V27_FAILURE_CODE','RESOURCE_OOM')
    execute_failure(database,lens_objects,monkeypatch,operation)
