"""No-DB fixture ownership regression, not runtime role qualification."""
from uuid import uuid4
import pytest
import wire_runtime_login as module


class Admin:
    def __init__(self,binding):self.binding=binding;self.calls=[]
    def __enter__(self):return self
    def __exit__(self,*args):return False
    def execute(self,query,args=None):
        self.calls.append((query,args))
        return self
    def fetchone(self):return self.binding


@pytest.mark.parametrize('pending',[False,True])
def test_login_binds_requested_phase_and_restores_exact_old_principal(monkeypatch,pending):
    tenant,worker,task=uuid4(),uuid4(),uuid4()
    admin=Admin((tenant,worker,'old_runtime'))
    monkeypatch.setattr(module.psycopg,'connect',lambda *a,**k:admin)
    with pytest.raises(RuntimeError,match='consumer failed'):
        with module.worker_login('host=localhost dbname=fixture user=admin',task,pending_dispatch=pending) as dsn:
            assert module.conninfo_to_dict(dsn)['user'].startswith('wire_worker_')
            updates=[args for query,args in admin.calls if isinstance(query,str) and query.startswith('UPDATE')]
            assert len(updates)==1 and updates[0][1:]==(tenant,worker)
            raise RuntimeError('consumer failed')
    selects=[query for query,args in admin.calls if isinstance(query,str) and query.startswith('SELECT')]
    assert len(selects)==1
    assert ('w.worker_id=t.dispatch_worker_id' in selects[0]) is pending
    if pending:
        assert "t.state='queued'" in selects[0] and 't.dispatch_command IS NOT NULL' in selects[0]
    else:
        assert 'w.worker_id=t.worker_id' in selects[0]
    updates=[args for query,args in admin.calls if isinstance(query,str) and query.startswith('UPDATE')]
    assert len(updates)==2
    assert updates[-1]==('old_runtime',tenant,worker,updates[0][0])
    assert len(admin.calls)==5+1  # CREATE,GRANT,SELECT,bind,restore,DROP


def test_pending_task_without_exact_dispatch_registration_never_yields_login(monkeypatch):
    admin=Admin(None)
    monkeypatch.setattr(module.psycopg,'connect',lambda *a,**k:admin)
    with pytest.raises(RuntimeError,match='WIRE_WORKER_REGISTRATION_MISSING'):
        with module.worker_login('host=localhost dbname=fixture user=admin',uuid4(),pending_dispatch=True):
            pytest.fail('unbound login escaped')
    assert not any(isinstance(query,str) and query.startswith('UPDATE') for query,args in admin.calls)
    assert len(admin.calls)==4  # CREATE,GRANT,SELECT,DROP even on lookup failure
