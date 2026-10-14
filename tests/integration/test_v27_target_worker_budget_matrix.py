"""Real target retry budget protocol; accelerated scheduler clock, injected errors."""
import pytest
from test_v27_target_worker_failure_matrix import lens_objects
from test_v27_target_worker_failure_matrix import test_target_worker_failure_recovery as run_target_case


@pytest.mark.parametrize('operation',('mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose'))
def test_target_worker_retry_budget(database,lens_objects,monkeypatch,operation):
    monkeypatch.delenv('PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE',raising=False)
    monkeypatch.setenv('PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST','1')
    run_target_case(database,lens_objects,monkeypatch,operation)
