from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.provenance_adoption_recovery import signing_adoption_committed


@pytest.mark.parametrize('row', [(True,), (False,), None, (1,), ('true',), ()])
def test_recovery_requires_strict_database_acknowledgement(row):
    tenant, task = uuid4(), uuid4()
    def execute(query, args):
        assert query == 'SELECT photo.v31_signing_adoption_committed(%s,%s)'
        assert args == (tenant, task)
        return SimpleNamespace(fetchone=lambda: row)
    connection = SimpleNamespace(autocommit=True, execute=execute)
    if row in ((True,), (False,)) and type(row[0]) is bool:
        assert signing_adoption_committed(connection, tenant, task, check_active=lambda: None) is row[0]
    else:
        with pytest.raises(AdoptionInvalid):
            signing_adoption_committed(connection, tenant, task, check_active=lambda: None)


@pytest.mark.parametrize('late', [False, True])
def test_recovery_false_activity_never_acknowledges_commit(late):
    queries, checks = [], []
    def execute(query, args):
        queries.append((query, args))
        return SimpleNamespace(fetchone=lambda: (True,))
    def active():
        checks.append(True)
        return late and len(checks) == 1
    connection = SimpleNamespace(autocommit=True, execute=execute)
    with pytest.raises(AdoptionInvalid, match='SIGNING_RECOVERY_INACTIVE'):
        signing_adoption_committed(connection, uuid4(), uuid4(), check_active=active)
    assert len(queries) == (1 if late else 0)
