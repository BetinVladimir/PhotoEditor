from uuid import uuid4

import pytest

from photo_worker.operations.capture.contracts import CapturePolicy, QACheck, QAReport
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.capture.quality import technical_quality


def fixtures():
    binding = VersionBinding(asset_version_id=uuid4(), source={'artifact_id': uuid4(), 'sha256': 'a' * 64}, canvas_hash='b' * 64)
    policy = CapturePolicy(schema_version=1, checks=['focus', 'clipping', 'duplicate'], max_queue_age_seconds=30,
                           calibration={'artifact_id': uuid4(), 'sha256': 'c' * 64})
    def check(code, score, face=None):
        return QACheck(code=code, status='pass', face_id=face, score=score, threshold=0.5, region=None, reason=None)
    report = QAReport(schema_version=1, binding=binding, checks=[check('focus', .9), check('clipping', .05), check('duplicate', 1.)])
    return policy, report, check


def test_quality_ignores_context_and_uses_worst_face():
    policy, report, check = fixtures()
    assert technical_quality(report, policy) == .9
    report.checks.append(check('focus', .6, uuid4()))
    assert technical_quality(report, policy) == .6


def test_missing_or_unavailable_evidence_is_not_clean_quality():
    policy, report, check = fixtures()
    report.checks.append(QACheck(code='focus', status='unavailable', face_id=uuid4(), score=None,
                                 threshold=None, region=None, reason='FACE_ROI_UNAVAILABLE'))
    assert technical_quality(report, policy) is None
    policy, report, check = fixtures()
    policy.checks.remove('clipping')
    report.checks = [item for item in report.checks if item.code != 'clipping']
    assert technical_quality(report, policy) is None


def test_quality_rejects_ambiguous_and_mismatched_reports():
    policy, report, check = fixtures()
    report.checks.append(check('focus', .8))
    with pytest.raises(ValueError, match='DUPLICATE_CHECK'):
        technical_quality(report, policy)
    policy, report, check = fixtures()
    report.checks = [item for item in report.checks if item.code != 'duplicate']
    with pytest.raises(ValueError, match='CHECK_COVERAGE'):
        technical_quality(report, policy)


def test_shared_postgresql_quality_vectors():
    import json
    from pathlib import Path
    vectors = json.loads((Path(__file__).parents[2] / 'backend/internal/v2capture/testdata/technical-quality.json').read_text())
    for vector in vectors:
        policy, report, _ = fixtures()
        policy = policy.model_copy(update={'checks': vector['requested']})
        report = report.model_copy(update={'checks': [QACheck.model_validate_json(json.dumps(item)) for item in vector['checks']]})
        if vector.get('error'):
            with pytest.raises(ValueError):
                technical_quality(report, policy)
        else:
            result = technical_quality(report, policy)
            if vector['quality'] is None:
                assert result is None, vector['name']
            else:
                assert result == pytest.approx(vector['quality']), vector['name']
