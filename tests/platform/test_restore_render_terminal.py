from contextlib import contextmanager
from datetime import datetime,timezone
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_batch_adoption import RestoreBatchAdoptionEvidence
from photo_worker.runtime.restore_render_review import RestoreRenderReviewEvidence
from photo_worker.runtime.restore_render_terminal import observe_fixture_render_terminal


@pytest.mark.parametrize('mode',['valid','missing','duplicate','bad_revision','foreign_database','cancelled','fixture_false','activity_false'])
def test_reviewed_render_terminal_requires_exact_parent_child_and_review(mode):
    tenant,actor,batch,child,workflow,review_id=(uuid4() for _ in range(6))
    adoption=RestoreBatchAdoptionEvidence('restore-batch-adoption/v1','isolated',batch,child,workflow)
    task=uuid5(workflow,'render.compose:0')
    candidate=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    review=RestoreRenderReviewEvidence('restore-render-review/v1','isolated',review_id,
        task,candidate,2,'b'*64,datetime.now(timezone.utc))
    row=(3,4 if mode!='bad_revision' else False)
    rows=[] if mode=='missing' else [row,row] if mode=='duplicate' else [row]
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            queries.append(sql)
            if 'FROM photo.runs parent' in sql:
                assert "parent.status='completed'" in sql and "child.status='completed'" in sql
                assert 'photo.v26_render_task_result_ready' in sql
                assert params==(child,task,tenant,batch,actor,workflow,review_id,actor,
                                'b'*64,2,candidate.artifact_id,candidate.sha256)
                return NS(fetchall=lambda:rows)
            if sql.startswith('SELECT current_database'):
                return NS(fetchone=lambda:('foreign' if mode=='foreign_database' else 'isolated',True))
            return NS()
    def active():
        if mode=='cancelled':raise RuntimeError('cancelled')
        if mode=='activity_false':return False
    def run():return observe_fixture_render_terminal(Connection(),tenant,actor,adoption,review,
        assert_fixture_scope=lambda:False if mode=='fixture_false' else None,check_active=active)
    if mode=='valid':
        observed=run()
        assert observed.batch_id==batch and observed.child_run_id==child
        assert observed.review_id==review_id and observed.parent_revision==3
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert queries==[]
