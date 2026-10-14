from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_batch_adoption import RestoreBatchAdoptionEvidence
from photo_worker.runtime.restore_batch_physical import RestoreBatchPhysicalEvidence
from photo_worker.runtime.restore_render_review_pending import find_fixture_pending_render_review


@pytest.mark.parametrize('mode',['valid','missing','duplicate','bad_revision','foreign_database','cancelled','fixture_false','activity_false'])
def test_pending_render_review_requires_exact_unique_adopted_candidate(mode):
    tenant,batch,child,workflow=(uuid4() for _ in range(4))
    adoption=RestoreBatchAdoptionEvidence('restore-batch-adoption/v1','isolated',batch,child,workflow)
    task=uuid5(workflow,'render.compose:0');candidate=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    result=NS(database='isolated',task_id=task,artifact_id=uuid4(),result_id=uuid4(),
              attempt=1,fence=2,object=NS(sha256='b'*64))
    physical=RestoreBatchPhysicalEvidence('restore-batch-physical/v1',result,
        NS(working_image=candidate),NS(),(),None,None,(),None)
    review=uuid4();row=(review,False if mode=='bad_revision' else 1)
    rows=[] if mode=='missing' else [row,row] if mode=='duplicate' else [row]
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            queries.append(sql)
            if 'FROM photo.reviews v' in sql:
                assert "v.state='pending'" in sql and "r.status='waiting_review'" in sql
                assert params[1:6]==(tenant,child,task,candidate.artifact_id,candidate.sha256)
                assert params[6]==workflow and params[7:10]==(result.result_id,1,2)
                return NS(fetchall=lambda:rows)
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('foreign' if mode=='foreign_database' else 'isolated',True))
            return NS()
    def active():
        if mode=='cancelled':raise RuntimeError('cancelled')
        if mode=='activity_false':return False
    def run():return find_fixture_pending_render_review(Connection(),tenant,adoption,physical,
        assert_fixture_scope=lambda:False if mode=='fixture_false' else None,check_active=active)
    if mode=='valid':
        observed=run()
        assert observed.review_id==review and observed.revision==1
        assert observed.candidate==candidate and observed.child_run_id==child
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert queries==[]
