from contextlib import contextmanager
from datetime import datetime,timezone
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_batch_adoption import RestoreBatchAdoptionEvidence
from photo_worker.runtime.restore_batch_physical import RestoreBatchPhysicalEvidence
from photo_worker.runtime.restore_render_review import observe_fixture_render_review


@pytest.mark.parametrize('mode',['valid','missing','duplicate','bad_hash','bad_revision','foreign_scope','fixture_false','activity_false'])
def test_render_review_requires_one_decided_adopted_candidate(mode):
    tenant,actor,batch,child,workflow=(uuid4() for _ in range(5))
    adoption=RestoreBatchAdoptionEvidence('restore-batch-adoption/v1','isolated',batch,child,workflow)
    task=uuid5(workflow,'render.compose:0');candidate=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    result=NS(database='isolated',task_id=task,artifact_id=uuid4(),result_id=uuid4(),
              attempt=1,fence=2,object=NS(sha256='b'*64))
    physical=RestoreBatchPhysicalEvidence('restore-batch-physical/v1',result,
        NS(working_image=candidate),NS(),(),None,None,(),None)
    review=uuid4();now=datetime.now(timezone.utc)
    row=(review,1 if mode=='bad_revision' else 2,
         'bad' if mode=='bad_hash' else 'c'*64,now)
    rows=[] if mode=='missing' else [row,row] if mode=='duplicate' else [row]
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            queries.append(sql)
            if 'FROM photo.reviews v' in sql:
                assert 'v.decision_route=\'candidate\'' in sql
                assert params[1:7]==(tenant,child,task,actor,candidate.artifact_id,candidate.sha256)
                assert params[7]==workflow and params[8:11]==(result.result_id,1,2)
                return NS(fetchall=lambda:rows)
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('other' if mode=='foreign_scope' else 'isolated',True))
            return NS()
    def run():return observe_fixture_render_review(Connection(),tenant,actor,adoption,physical,
        assert_fixture_scope=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        observed=run()
        assert observed.review_id==review and observed.candidate==candidate
        assert observed.task_id==task and observed.decision_sha256=='c'*64
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert queries==[]
