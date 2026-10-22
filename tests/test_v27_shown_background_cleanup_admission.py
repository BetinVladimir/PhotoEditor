"""Result-scoped admission rejects invalid metadata before object IO."""
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from photo_worker.v2.shown_background_cleanup_admission import sql_shown_background_cleanup_adoption_admission


@pytest.mark.parametrize('size', [None, True, False, 2.0, '2', -1, 0, 1, 1048577])
def test_shown_cleanup_admission_rejects_invalid_size_before_json_read(size):
    ref = ArtifactRef(uuid4(), 'a' * 64)
    calls = []
    class Connection:
        def execute(self, statement, args):
            calls.append(statement)
            assert 'v27_shown_background_cleanup_adoption_params' in statement
            return SimpleNamespace(fetchone=lambda: (ref.artifact_id, ref.sha256))
    class Reader:
        def evidence(self, requested):
            assert requested == ref
            return ArtifactEvidence(ref, 'application/json', size, None, 'input')
        def read_json(self, *args):
            raise AssertionError('invalid metadata must stop before object IO')
    with pytest.raises(AdoptionInvalid, match='SEMANTIC_SHOWN_EVIDENCE_INVALID'):
        sql_shown_background_cleanup_adoption_admission(Connection(), uuid4(), uuid4(),
            SimpleNamespace(attempt=1, fence=1, result_id=uuid4()), Reader(), check_active=lambda: True)
    assert len(calls) == 1


@pytest.mark.parametrize('size', [2, 1048576])
def test_shown_cleanup_admission_accepts_size_boundaries_for_bounded_read(size):
    ref = ArtifactRef(uuid4(), 'a' * 64)
    class ReachedBoundedRead(Exception):
        pass
    class Connection:
        def execute(self, statement, args):
            return SimpleNamespace(fetchone=lambda: (ref.artifact_id, ref.sha256))
    class Reader:
        def evidence(self, requested):
            assert requested == ref
            return ArtifactEvidence(ref, 'application/json', size, None, 'input')
        def read_json(self, requested, maximum):
            assert requested == ref and maximum == 1048576
            raise ReachedBoundedRead
    with pytest.raises(ReachedBoundedRead):
        sql_shown_background_cleanup_adoption_admission(Connection(), uuid4(), uuid4(),
            SimpleNamespace(attempt=1, fence=1, result_id=uuid4()), Reader(), check_active=lambda: True)
