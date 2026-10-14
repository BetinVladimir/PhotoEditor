from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import psycopg
import pytest
from photo_worker.runtime.restore_batch_result import observe_fixture_batch_result
from photo_worker.runtime.restore_batch_adoption import RestoreBatchAdoptionEvidence


@pytest.mark.parametrize('mode',['valid','foreign_database','not_ready','invalid_attempt','foreign_key','oversized','scope_false','activity_false'])
def test_worker_baseline_requires_canonical_success_attempt_and_scoped_root(mode):
    tenant=uuid4();a=RestoreBatchAdoptionEvidence('restore-batch-adoption/v1','isolated',uuid4(),uuid4(),uuid4());queries=[]
    row=(uuid4(),uuid4(),0 if mode=='invalid_attempt' else 1,7,'photos',
        'tenants/'+str(uuid4() if mode=='foreign_key' else tenant)+'/result.json','v1','a'*64,
        1048577 if mode=='oversized' else 128)
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            queries.append(q);value=None
            if q.startswith('SELECT current_database'):value=('other' if mode=='foreign_database' else 'isolated',True)
            if q.startswith('SELECT z.id'):
                assert params==(tenant,a.child_run_id,uuid5(a.workflow_id,'render.compose:0'))
                value=None if mode=='not_ready' else row
            return NS(fetchone=lambda:value)
    def run():return observe_fixture_batch_result(Connection(),tenant,a,
        assert_fixture_scope=lambda *args:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().fence==7
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert not any(q.startswith(('UPDATE','DELETE','INSERT')) for q in queries)
    if mode in ('scope_false','activity_false'):assert queries==[]
