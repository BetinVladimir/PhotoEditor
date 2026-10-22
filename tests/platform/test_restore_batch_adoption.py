from contextlib import contextmanager
from types import SimpleNamespace as NS
from datetime import datetime,timezone
from hashlib import sha256
from dataclasses import replace
from uuid import uuid4,uuid5
import psycopg
import pytest
from photo_worker.runtime import restore_batch_adoption as m


@pytest.mark.parametrize('mode',['valid','foreign_ticket','wrong_request','foreign_database','missing_graph','scope','scope_false','activity_false'])
def test_batch_ticket_requires_independent_registered_graph(monkeypatch,mode):
    t=m.RestoreRenderBatchTarget('https://restore.test',uuid4(),uuid4(),uuid4(),'a'*64,'positive','Bearer private')
    b=m.RestoreFreezeBaselineBinding(uuid4(),uuid4(),uuid4(),'b'*64,'c'*64);now=datetime.now(timezone.utc)
    f=m.RestoreFreezeBaselineEvidence('restore-freeze-baseline/v1',uuid4(),uuid4(),'d'*64,'e'*64,'f'*64,now)
    batch=uuid5(t.tenant_id,'V2.Admit:'+str(b.actor_id)+':positive');child=uuid5(batch,'render.batch:0')
    ticket=m.RestoreRenderBatchBaseline('restore-render-batch-baseline/v1',batch,child,'queued',
        sha256(m.render_batch_request_body(t,b,f)).hexdigest(),'a'*64,now)
    if mode=='foreign_ticket':ticket=replace(ticket,child_run_id=uuid4())
    if mode=='wrong_request':ticket=replace(ticket,request_sha256='0'*64)
    queries=[];session=uuid4()
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            queries.append(q);row=None
            if q.startswith('SELECT current_database'):row=('foreign' if mode=='foreign_database' else 'isolated',True)
            elif q.startswith('SELECT EXISTS'):
                assert params[0]==child and params[5]==batch and params[11]==str(session)
                row=(mode!='missing_graph',)
            return NS(fetchone=lambda:row)
    def scope(*args):
        if mode=='scope':raise RuntimeError('scope lost')
        if mode=='scope_false':return False
    def run():return m.verify_fixture_batch_adoption(Connection(),t,b,f,ticket,expected_database='isolated',
        session_id=session,assert_fixture_scope=scope,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().workflow_id==uuid5(child,'workflow')
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('foreign_ticket','wrong_request','scope','scope_false','activity_false'):assert not queries
    assert not any(q.startswith(('INSERT','UPDATE','DELETE')) for q in queries)
