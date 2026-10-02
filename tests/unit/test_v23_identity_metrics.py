from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.identity.metrics import (
    compare_identity_observations, measure_pixel_guards,
)
from photo_worker.operations.session.contracts import (
    FaceObservation, FaceObservations, FacePoint, FaceRect, VersionBinding,
)


def _face(*, x=.2, y=.2, embedding=True, quality=1.0):
    vector = [0.0] * 512
    vector[0] = 1.0
    return FaceObservation(observation_id=uuid4(), face_id=uuid4(),
                           box=FaceRect(x0=x, y0=y, x1=x + .2, y1=y + .2),
                           landmarks=[FacePoint(x=x + .1, y=y + .1)] * 68,
                           embedding=vector if embedding else None,
                           quality=quality, person_id=None)


def _observations(faces, canvas='a' * 64):
    return FaceObservations(schema_version=1,
                            binding=VersionBinding(asset_version_id=uuid4(),
                                                   source={'artifact_id': uuid4(),
                                                           'sha256': 'b' * 64},
                                                   canvas_hash=canvas), faces=faces)


def _compare(before, after, **changes):
    options = dict(detector_qualified=True, consent_valid=True,
                   aligned_canvas=True,
                   canvas_size=(512, 512),
                   outside_mask_max_error=0.0, protected_max_error=0.0)
    options.update(changes)
    calibration = SimpleNamespace(qualified=True, identity_similarity_min=.8,
                                  landmark_drift_max=.1,min_face_edge_px=32)
    return compare_identity_observations(_observations(before),
                                         _observations(after), calibration, **options)


def test_matching_identity_passes_and_protected_change_fails():
    before, after = _face(), _face()
    assert _compare([before], [after]).status == 'pass'
    assert _compare([before], [after], protected_max_error=1e-9).status == 'fail'


def test_missing_consent_embedding_or_detector_never_passes():
    before, after = _face(), _face()
    assert _compare([before], [after], consent_valid=False).status == 'not_measurable'
    assert _compare([before], [_face(embedding=False)]).status == 'not_measurable'
    assert _compare([before], [after], detector_qualified=False).status == 'not_measurable'
    assert _compare([before], [after], aligned_canvas=False).status == 'not_measurable'
    assert _compare([before], [after], canvas_size=(100, 100)).status == 'not_measurable'
    assert _compare([], []).status == 'not_measurable'


def test_pairing_requires_overlap_and_reports_count_mismatch():
    before = _face(x=.1)
    assert _compare([before], [_face(x=.7)]).status == 'review'
    assert _compare([before], [_face(x=.1), _face(x=.7)]).status == 'review'


def test_embedding_and_landmark_regressions_fail():
    before = _face()
    changed = _face()
    changed.embedding[0] = 0.0
    changed.embedding[1] = 1.0
    assert _compare([before], [changed]).status == 'fail'
    shifted = _face()
    shifted.landmarks = [FacePoint(x=.37, y=.37)] * 68
    assert _compare([before], [shifted]).status == 'fail'


def test_aligned_different_source_versions_can_be_compared():
    before, after = _face(), _face()
    calibration = SimpleNamespace(qualified=True, identity_similarity_min=.8,
                                  landmark_drift_max=.1, min_face_edge_px=32)
    result = compare_identity_observations(
        _observations([before], 'a' * 64),
        _observations([after], 'b' * 64), calibration,
        detector_qualified=True, consent_valid=True,
        aligned_canvas=True, canvas_size=(512, 512),
        outside_mask_max_error=0.0, protected_max_error=0.0)
    assert result.status == 'pass'


def test_pixel_guards_cover_soft_protection_and_outside_mask_exactly():
    source = np.zeros((3, 4, 4), dtype=np.float32)
    result = source.copy()
    result[0, 0, 0] = .25
    result[1, 1, 1] = .125
    effective = np.zeros((3, 4), dtype=np.float32)
    effective[1, 1] = 1
    protected = np.zeros((3, 4), dtype=np.float32)
    protected[1, 1] = .001
    guards = measure_pixel_guards(source, result, effective, protected,
                                  tile=128)
    assert guards.outside_mask_max_error == .25
    assert guards.protected_max_error == .125
    assert _compare([_face()], [_face()],
                    outside_mask_max_error=guards.outside_mask_max_error,
                    protected_max_error=guards.protected_max_error).status == 'fail'
    protected[1, 1] = 0
    with pytest.raises(ValueError, match='INVALID_IDENTITY_PIXELS'):
        measure_pixel_guards(source, result, effective,
                             np.full((3, 4), np.nan), tile=128)


def test_invalid_canvas_and_duplicate_identity_rejected():
    face = _face()
    with pytest.raises(ValueError, match='DUPLICATE_FACE_ID'):
        _compare([face, face], [_face()])
    calibration = SimpleNamespace(qualified=True, identity_similarity_min=.8,
                                  landmark_drift_max=.1,min_face_edge_px=32)
    with pytest.raises(ValueError, match='INVALID_IDENTITY_INPUT'):
        compare_identity_observations(_observations([face], 'a' * 64),
                                      _observations([_face()], 'c' * 64),
                                      calibration, detector_qualified=True,
                                      consent_valid=True, aligned_canvas=True,
                                      canvas_size=(0,512),
                                      outside_mask_max_error=0,
                                      protected_max_error=0)
