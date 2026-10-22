"""Result-scoped admission rejects invalid metadata before object IO."""
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from photo_worker.v2.shown_retouch_admission import sql_shown_retouch_adoption_admission


@pytest.mark.parametrize('size', [None, True, False, 2.0, '2', -1, 0, 1, 1048577])
def test_shown_retouch_admission_rejects_invalid_size_before_json_read(size):
    ref = ArtifactRef(uuid4(), 'a' * 64)
    calls = []
    class Connection:
        def execute(self, statement, args):
            calls.append(statement)
            assert 'v27_shown_retouch_adoption_params' in statement
            return SimpleNamespace(fetchone=lambda: (ref.artifact_id, ref.sha256))
    class Reader:
        def evidence(self, requested):
            assert requested == ref
            return ArtifactEvidence(ref, 'application/json', size, None, 'input')
        def read_json(self, *args):
            raise AssertionError('invalid metadata must stop before object IO')
    with pytest.raises(AdoptionInvalid, match='SEMANTIC_SHOWN_EVIDENCE_INVALID'):
        sql_shown_retouch_adoption_admission(Connection(), uuid4(), uuid4(),
            SimpleNamespace(attempt=1, fence=1, result_id=uuid4(), operation='semantic.adopt_shown_retouch_region', version='1'), Reader(), check_active=lambda: True)
    assert len(calls) == 1


@pytest.mark.parametrize('size', [2, 1048576])
def test_shown_retouch_admission_accepts_size_boundaries_for_bounded_read(size):
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
        sql_shown_retouch_adoption_admission(Connection(), uuid4(), uuid4(),
            SimpleNamespace(attempt=1, fence=1, result_id=uuid4(), operation='semantic.adopt_shown_retouch_region', version='1'), Reader(), check_active=lambda: True)


@pytest.mark.parametrize('mutation', [dict(operation='semantic.adopt_shown_background_cleanup'),
    dict(version='2'), dict(attempt=True), dict(attempt=2147483648),
    dict(fence=9223372036854775808), dict(result_id=None)])
def test_shown_retouch_admission_rejects_foreign_or_malformed_context_before_io(mutation):
    context=SimpleNamespace(attempt=1,fence=1,result_id=uuid4(),
        operation='semantic.adopt_shown_retouch_region',version='1')
    for key,value in mutation.items():setattr(context,key,value)
    class NoIO:
        def execute(self,*args):raise AssertionError('SQL after invalid context')
        def evidence(self,*args):raise AssertionError('object IO after invalid context')
    with pytest.raises(AdoptionInvalid,match='CONTEXT_REQUIRED'):
        sql_shown_retouch_adoption_admission(NoIO(),uuid4(),uuid4(),context,NoIO(),check_active=lambda:True)
