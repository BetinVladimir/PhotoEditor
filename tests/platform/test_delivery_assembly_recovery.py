from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.delivery_assembly_recovery import delivery_assembly_adoption_committed


@pytest.mark.parametrize('row', [(True,), (False,), None, (1,), ('true',), (True, False)])
def test_recovery_requires_exact_boolean_receipt(row):
    tenant, task = uuid4(), uuid4()
    calls = []
    class Connection:
        autocommit = True
        def execute(self, query, args):
            calls.append('sql')
            assert 'v30_delivery_assembly_adoption_committed' in query
            assert args == (tenant, task)
            return self
        def fetchone(self): return row
    check = lambda: calls.append('active')
    if row in ((True,), (False,)) and type(row[0]) is bool:
        assert delivery_assembly_adoption_committed(Connection(), tenant, task, check_active=check) is row[0]
    else:
        with pytest.raises(AdoptionInvalid):
            delivery_assembly_adoption_committed(Connection(), tenant, task, check_active=check)
    assert calls == ['active', 'sql', 'active']


def test_service_acknowledges_closed_context_without_object_reads(monkeypatch):
    from photo_worker.v2.adoption_service import AdoptionCoordinator
    class Connection:
        autocommit = True
        def execute(self, query, args):
            assert 'v30_delivery_assembly_adoption_committed' in query
            return self
        def fetchone(self): return (True,)
    coordinator = AdoptionCoordinator(Connection(), object(), object(), 'photos')
    def closed(*args): raise AdoptionInvalid('V2_ADOPTION_CONTEXT_STALE')
    monkeypatch.setattr(coordinator, 'context', closed)
    assert coordinator.adopt(uuid4(), uuid4()) is True
