"""Actual scheduled/reclaimed target retry; injected errors, model doubles."""
import pytest
from test_v27_target_worker_failure_matrix import lens_objects
from test_v27_target_worker_failure_matrix import test_target_worker_failure_recovery as run_target_case


@pytest.mark.parametrize('operation',('mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose'))
def test_target_worker_retry_once(database,lens_objects,monkeypatch,operation):
    monkeypatch.setenv('PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE','1')
    run_target_case(database,lens_objects,monkeypatch,operation)
