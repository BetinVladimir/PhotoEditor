"""Real admitted/claimed target failure protocol; injected errors, model doubles."""
import pytest
from test_v27_semantic_intent_publication import lens_objects
from test_v27_semantic_intent_publication import test_semantic_intent_publication_old_version_and_replay as run_native_case


@pytest.mark.parametrize('operation',('mask.person','face.detect','face.landmarks','face.parse','mask.face_skin','defects.propose'))
def test_target_worker_failure_recovery(database,lens_objects,monkeypatch,operation):
    flags='TARGET_DEFECT_ADVANCE TARGET_SKIN_ADVANCE TARGET_SKIN_PLAN TARGET_FACE_FINISH TARGET_FACE_EXECUTE TARGET_FACE_PLAN TARGET_DETECTOR_FINISH TARGET_DETECTOR_EXECUTE TARGET_DETECTOR_SEAL TARGET_DETECTOR_REGISTER TARGET_DETECTOR_DRAFT TARGET_PERSON_ADVANCE TARGET_PERSON_DRAFT PERSON_DEFECT_PARENT PERSON_DEFECT_FRESH ORIGINAL_PERSON'.split()
    for flag in flags:
        monkeypatch.setenv('PHOTO_TEST_V27_'+flag,'1')
    monkeypatch.setenv('PHOTO_TEST_V27_TARGET_WORKER_FAILURE_OPERATION',operation)
    run_native_case(database,lens_objects,True,'native-person-defect-wire')


def test_target_worker_success_regression(database,lens_objects,monkeypatch):
    # The same prepared target chain must still complete with failure hooks off.
    test_target_worker_failure_recovery(database,lens_objects,monkeypatch,'')
