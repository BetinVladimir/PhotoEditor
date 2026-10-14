from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_parse_adoption_recovery import interchange_parse_adoption_committed


@pytest.mark.parametrize('row', [(True,), (False,), None, (1,), (0,), ('true',), (True, False)])
def test_recovery_accepts_only_exact_boolean_receipt(row):
    calls = []
    def execute(statement, values):
        calls.append((statement, values))
        return SimpleNamespace(fetchone=lambda: row)
    db = SimpleNamespace(autocommit=True, execute=execute)
    active = []
    tenant, task = uuid4(), uuid4()
    call = lambda: interchange_parse_adoption_committed(db, tenant, task, check_active=lambda: active.append(True))
    if row in ((True,), (False,)) and type(row[0]) is bool:
        assert call() is row[0]
    else:
        with pytest.raises(AdoptionInvalid):
            call()
    assert calls[0][1] == (tenant, task)
    assert len(active) == 2


def test_recovery_cancelled_after_read_never_acknowledges():
    state = []
    def active():
        if state:
            raise RuntimeError('cancelled')
    def execute(*args):
        state.append(True)
        return SimpleNamespace(fetchone=lambda: (True,))
    db = SimpleNamespace(autocommit=True, execute=execute)
    with pytest.raises(RuntimeError, match='cancelled'):
        interchange_parse_adoption_committed(db, uuid4(), uuid4(), check_active=active)
