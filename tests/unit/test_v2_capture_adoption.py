from uuid import UUID

import pytest

from photo_worker.operations.capture.contracts import QACheck, QAReport
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.capture_adoption import report_severity


@pytest.mark.parametrize('statuses,expected', [(['pass'], 'info'),
    (['unavailable'], 'warning'), (['pass', 'warning'], 'warning'),
    (['unavailable', 'critical'], 'critical')])
def test_capture_severity_never_treats_unavailable_as_clean(statuses, expected):
    binding = VersionBinding(asset_version_id=UUID(int=1),
        source=ArtifactRef(artifact_id=UUID(int=2), sha256='a'*64), canvas_hash='b'*64)
    checks = [QACheck(code='eyes', status=status, face_id=None,
        score=None if status=='unavailable' else .5, threshold=.6, region=None,
        reason='MODEL_UNAVAILABLE' if status=='unavailable' else None) for status in statuses]
    assert report_severity(QAReport(schema_version=1,binding=binding,checks=checks)) == expected


def test_unavailable_report_cannot_carry_fabricated_score():
    with pytest.raises(ValueError, match='INVALID_UNAVAILABLE_CHECK'):
        QACheck(code='eyes',status='unavailable',face_id=None,score=1.,threshold=None,
                region=None,reason='MODEL_UNAVAILABLE')
