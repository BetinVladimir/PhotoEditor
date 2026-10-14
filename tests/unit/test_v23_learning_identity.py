from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.learning.identity import (
    confirmed_source_faces, match_after_faces, measure_holdout_identity,
)
from photo_worker.operations.session.contracts import (
    ConsentSnapshot, FaceObservation, FaceObservations, FacePoint,
    FaceRect, VersionBinding,
)


def _evidence(faces=(), *, qualified=True, succeeded=True, consent=True,
              aligned=True, changed=False, protected=False):
    binding = VersionBinding(asset_version_id=uuid4(),
                             source={'artifact_id': uuid4(), 'sha256': 'a' * 64},
                             canvas_hash='b' * 64)
    before = FaceObservations(schema_version=1, binding=binding,
                              faces=list(faces))
    after = FaceObservations(schema_version=1, binding=binding,
                             faces=list(faces))
    source = np.zeros((256, 256, 3), dtype=np.float32)
    candidate = source.copy()
    mask = np.ones((256, 256), dtype=np.float32)
    guard = np.zeros((256, 256), dtype=np.float32)
    if changed:
        candidate[0, 0] = 0.01
        mask[0, 0] = 0
    if protected:
        candidate[0, 0] = 0.01
        guard[0, 0] = 0.001
    calibration = SimpleNamespace(qualified=True, identity_similarity_min=.8,
                                  landmark_drift_max=.1, min_face_edge_px=32)
    return dict(before=before, after=after, source_binding=binding,
                candidate_binding=binding, source_rgb=source,
                candidate_rgb=candidate, effective_mask=mask,
                protected=guard, calibration=calibration,
                detector_qualified=qualified, detector_succeeded=succeeded,
                consent_valid=consent, aligned_canvas=aligned)


def _face():
    point = FacePoint(x=.3, y=.3)
    return FaceObservation(observation_id=uuid4(), face_id=uuid4(),
                           box=FaceRect(x0=.2, y0=.2, x1=.4, y1=.4),
                           landmarks=[point] * 68,
                           embedding=[1.] + [0.] * 511,
                           quality=1, person_id=uuid4())


def test_empty_successful_detection_is_no_person_only_with_zero_guards():
    assert measure_holdout_identity(**_evidence()) == 'no_person'
    for change in ({'qualified': False}, {'succeeded': False},
                   {'aligned': False}):
        assert measure_holdout_identity(**_evidence(**change)) == 'not_measurable'
    assert measure_holdout_identity(**_evidence(changed=True)) == 'fail'
    assert measure_holdout_identity(**_evidence(protected=True)) == 'fail'


def test_holdout_identity_fails_on_alpha_only_protected_change():
    evidence = _evidence()
    evidence['source_alpha'] = np.ones((256, 256), np.float32)
    evidence['candidate_alpha'] = evidence['source_alpha'].copy()
    evidence['candidate_alpha'][0, 0] = .5
    evidence['protected'][0, 0] = .001
    assert measure_holdout_identity(**evidence) == 'fail'


def test_face_verdict_requires_consent_and_matching_observations():
    face = _face()
    assert measure_holdout_identity(**_evidence([face])) == 'pass'
    assert measure_holdout_identity(**_evidence([face], consent=False)) == 'not_measurable'
    evidence = _evidence([face])
    evidence['after'] = FaceObservations(schema_version=1,
                                        binding=evidence['source_binding'], faces=[])
    assert measure_holdout_identity(**evidence) == 'not_measurable'


def test_binding_mismatch_is_rejected():
    evidence = _evidence()
    evidence['candidate_binding'] = evidence['candidate_binding'].model_copy(
        update={'asset_version_id': uuid4()})
    with pytest.raises(ValueError, match='INVALID_HOLDOUT_IDENTITY_BINDING'):
        measure_holdout_identity(**evidence)


def test_confirmed_source_face_requires_exact_grant_and_geometric_match():
    evidence = _evidence()
    binding = evidence['source_binding']
    face = _face()
    before = FaceObservations(schema_version=1, binding=binding, faces=[face])
    consent_id = uuid4()
    snapshot = ConsentSnapshot(schema_version=1, grants=[dict(
        consent_id=consent_id, epoch='3', person_id=face.person_id,
        purpose='identity_matching')], confirmed_faces=[dict(
        asset_version_id=binding.asset_version_id, face_id=face.face_id,
        person_id=face.person_id, consent_id=consent_id, epoch='3')])
    confirmed = confirmed_source_faces(before, snapshot)
    assert confirmed == {face.face_id: face.person_id}
    target = face.model_copy(update={'face_id': uuid4()})
    after = FaceObservations(schema_version=1, binding=binding, faces=[target])
    assert match_after_faces(before, after, confirmed) == {
        target.face_id: face.person_id}
    wrong = target.model_copy(update={
        'box': FaceRect(x0=.7, y0=.7, x1=.9, y1=.9)})
    assert match_after_faces(before, after.model_copy(update={'faces': [wrong]}),
                             confirmed) is None
    revoked = snapshot.model_copy(update={'grants': []})
    with pytest.raises(ValueError, match='INVALID_CONFIRMED_FACE_CONSENT'):
        confirmed_source_faces(before, revoked)
