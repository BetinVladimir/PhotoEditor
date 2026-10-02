import json
from pathlib import Path
from uuid import UUID

import numpy as np
import pytest

from photo_worker.operations.capture.assess import assess_capture
from photo_worker.operations.capture.contracts import CaptureContext, CapturePolicy, ShotList
from photo_worker.operations.session.contracts import Calibration, FaceObservations, VersionBinding


def inputs():
    fixtures = json.loads((Path(__file__).resolve().parents[2] /
        'docs/implementation-v2/contracts/structural-fixtures.json').read_text())['fixtures']
    binding = VersionBinding.model_validate_json(json.dumps(fixtures['VersionBinding']))
    calibration = dict(fixtures['Calibration'], qualified=True, loss_weights=[.4,.1,.1,.1,.2,.1], focus_critical=.1,
        focus_warning=.2, clipping_warning_fraction=.1, clipping_critical_fraction=.2)
    calibration = Calibration.model_validate_json(json.dumps(calibration))
    ref = fixtures['ArtifactRef']
    policy = CapturePolicy.model_validate_json(json.dumps(dict(schema_version=1,
        checks=['focus', 'eyes', 'clipping', 'glare', 'duplicate', 'shot_list'],
        max_queue_age_seconds=30, calibration=ref)))
    requirement = UUID(int=9)
    shots = ShotList.model_validate(dict(schema_version=1, requirements=[dict(id=requirement,
        label='portrait', person_ids=[UUID(int=8)], min_confirmed_frames=1, min_quality=.5)]))
    context = CaptureContext.model_validate_json(json.dumps(dict(schema_version=1,
        session_id=str(UUID(int=7)), confirmed_person_ids=[str(UUID(int=8))],
        shot_list=ref, recent_content_shas=[binding.source.sha256], consents=ref,
        coverage=[dict(requirement_id=str(requirement), confirmed_frames=0)])))
    faces = FaceObservations(schema_version=1, binding=binding, faces=[])
    return binding, faces, policy, calibration, context, shots


def test_missing_faces_and_models_are_unavailable_and_current_frame_is_not_confirmed():
    binding, faces, policy, calibration, context, shots = inputs()
    report = assess_capture(np.full((64, 64, 3), .5), binding, faces, policy,
                            calibration, context, shots, calibration.model_lock_sha, binding.source.sha256)
    checks = {check.code: check for check in report.checks}
    assert checks['focus'].status == 'critical'
    assert checks['clipping'].status == 'pass'
    assert checks['eyes'].status == checks['glare'].status == 'unavailable'
    assert checks['eyes'].score is None
    assert checks['duplicate'].status == 'warning'
    assert checks['shot_list'].status == 'warning'
    assert checks['shot_list'].score == 0


def test_binding_lock_and_frozen_coverage_must_match():
    binding, faces, policy, calibration, context, shots = inputs()
    rgb = np.full((64, 64, 3), .5)
    with pytest.raises(ValueError, match='UNQUALIFIED'):
        assess_capture(rgb, binding, faces, policy, calibration, context, shots, 'f' * 64, binding.source.sha256)
    context.coverage = []
    with pytest.raises(ValueError, match='COVERAGE_MISMATCH'):
        assess_capture(rgb, binding, faces, policy, calibration, context, shots, calibration.model_lock_sha, binding.source.sha256)
    with pytest.raises(ValueError, match='DUPLICATE_CAPTURE_CHECK'):
        CapturePolicy(schema_version=1, checks=['focus', 'focus'], max_queue_age_seconds=30,
                      calibration=policy.calibration)


def test_duplicate_uses_original_content_sha_instead_of_working_document_sha():
    binding, faces, policy, calibration, context, shots = inputs()
    report = assess_capture(np.full((64, 64, 3), .5), binding, faces, policy,
        calibration, context, shots, calibration.model_lock_sha, 'b' * 64)
    assert next(check for check in report.checks if check.code == 'duplicate').status == 'pass'
