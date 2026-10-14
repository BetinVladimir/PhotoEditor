"""Real retry-to-success target protocol; actual backoff, model doubles."""
import pytest
from test_v27_target_worker_failure_matrix import lens_objects
from test_v27_target_worker_failure_matrix import test_target_worker_success_regression as execute_native_chain


@pytest.mark.parametrize('operation',('mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose'))
def test_target_worker_retry_to_success(database,lens_objects,monkeypatch,operation):
    for flag in ('PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE','PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST','PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE'):
        monkeypatch.delenv(flag,raising=False)
    monkeypatch.setenv('PHOTO_TEST_V27_TARGET_WORKER_RETRY_SUCCESS_OPERATION',operation)
    execute_native_chain(database,lens_objects,monkeypatch)
