from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import psycopg
import pytest
from photo_worker.runtime.restore_batch_catalogue import verify_fixture_batch_not_adopted
from photo_worker.runtime.restore_revoke_batch import RestoreRenderBatchTarget
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeBaselineBinding


@pytest.mark.parametrize('mode',['valid','foreign_database','not_ready','bad_snapshot','run','workflow','task','idempotency','scope_false','activity_false'])
def test_batch_catalogue_requires_live_exact_snapshot_and_no_partial_adoption(mode):
    t=RestoreRenderBatchTarget('https://restore.test',uuid4(),uuid4(),uuid4(),'a'*64,'fresh','Bearer private')
    b=RestoreFreezeBaselineBinding(uuid4(),uuid4(),uuid4(),'b'*64,'c'*64);queries=[];session=uuid4()
    batch=uuid5(t.tenant_id,'V2.Admit:'+str(b.actor_id)+':fresh')
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            queries.append(q);row=None
            if q.startswith('SELECT current_database'):row=('other' if mode=='foreign_database' else 'isolated',True)
            elif q.startswith('SELECT photo.v25_lens_snapshot_ready'):
                assert params==(t.tenant_id,t.project_id,t.snapshot_id,b.actor_id,session)
                row=(mode!='not_ready',)
            elif q.startswith('SELECT EXISTS(SELECT FROM photo.snapshots'):
                assert params==(t.tenant_id,t.snapshot_id,b.actor_id,b.asset_version_id,b.source_artifact_id,b.source_sha256,t.project_id)
                row=(mode!='bad_snapshot',)
            elif q.startswith('SELECT\n EXISTS'):
                assert params[1]==batch and params[-1]=='fresh'
                row=tuple(mode==name for name in ('run','workflow','task','idempotency'))
            return NS(fetchone=lambda:row)
    def run():return verify_fixture_batch_not_adopted(Connection(),t,b,expected_database='isolated',session_id=session,
        assert_fixture_scope=lambda *args:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().batch_id==batch
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert not any(q.startswith(('DELETE','UPDATE','INSERT')) for q in queries)
    if mode in ('scope_false','activity_false'):assert queries==[]
