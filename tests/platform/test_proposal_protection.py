import numpy as np
import pytest
from photo_worker.operations.defects.protection import verify_proposal_protection
from photo_worker.operations.portrait.prepare import FaceInput
from test_skin_mask import fixture
from test_propose_prepare import fixture as proposal_fixture


@pytest.mark.parametrize('manual_mode', [False, True])
@pytest.mark.parametrize('point', [(90,104), (166,104), (128,135), (128,179), (90,82)])
def test_each_feature_requires_full_protection_in_both_modes(manual_mode, point):
    from photo_worker.algorithms.skin_mask import build_skin_mask
    face, roi, probability = fixture()
    _, image, _ = proposal_fixture()
    image = image.model_copy(update={'canvas': face.canvas, 'transform_chain': face.transform_chain})
    entry = FaceInput(face, None if manual_mode else probability,
                      probability if manual_mode else None, manual_mode)
    derived = build_skin_mask(face, roi, entry.skin_probability,
                              manual=entry.manual, manual_mode=manual_mode)
    protection = derived.protected.copy()
    labels = np.zeros((256,256), np.uint16)
    verify_proposal_protection(image, [entry], protection, labels)
    x, y = point
    assert protection[y,x] == 1
    protection[y,x] = .999
    with pytest.raises(ValueError, match='facial feature'):
        verify_proposal_protection(image, [entry], protection, labels)


def test_partial_protection_candidate_and_cancellation():
    _, image, _ = proposal_fixture()
    protection = np.zeros((512,512),np.float32)
    labels = np.zeros((512,512),np.uint16)
    protection[-1,-1] = .001
    labels[-1,-1] = 1
    with pytest.raises(ValueError, match='intersects'):
        verify_proposal_protection(image, [], protection, labels)
    labels[-1,-1] = 0
    verify_proposal_protection(image, [], protection, labels)
    calls = 0
    def cancel():
        nonlocal calls
        calls += 1
        if calls == 3: raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        verify_proposal_protection(image, [], protection, labels, check_active=cancel)
    with pytest.raises(ValueError, match='budget'):
        verify_proposal_protection(image, [], protection, labels, max_roi_pixels=True)
