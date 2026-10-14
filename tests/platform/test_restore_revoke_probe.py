from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture,observe_fixture_learning_consent


@pytest.mark.parametrize('mode',['baseline','revoked','foreign_database','unprivileged','wrong_epoch',
    'wrong_actor','missing_lineage','inactive_baseline','missing_event','active_version','live_workflow','scope_failure','scope_false','activity_false'])
def test_revoke_observer_requires_commit_lineage_and_disabled_dependencies(mode):
    fixture=RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    revoked=mode not in ('baseline','inactive_baseline');queries=[];events=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            try:yield
            except BaseException:events.append('rollback');raise
            else:events.append('commit')
        def execute(self,statement,params=None):
            queries.append(statement);row=None
            if statement.startswith('SELECT current_database'):
                row=('foreign' if mode=='foreign_database' else 'isolated',mode!='unprivileged')
            elif statement.startswith('SELECT g.purpose'):
                row=(fixture.purpose,fixture.person_id,'revoked' if revoked else 'granted',
                    fixture.epoch+int(revoked)+(1 if mode=='wrong_epoch' else 0),fixture.revision+int(revoked),
                    fixture.policy_version,uuid4() if mode=='wrong_actor' else fixture.actor_id)
            elif statement.startswith('SELECT EXISTS(SELECT FROM photo.learning_example_links'):row=(mode!='missing_lineage',)
            elif statement.startswith('SELECT EXISTS(SELECT FROM photo.learning_versions'):row=(mode!='inactive_baseline',)
            elif statement.startswith('SELECT EXISTS(SELECT FROM photo.consent_events'):row=(mode!='missing_event',)
            elif statement.startswith('WITH affected'):row=(2,1 if mode=='active_version' else 0)
            elif statement.startswith('SELECT EXISTS(SELECT FROM photo.workflow_consent_links'):row=(mode=='live_workflow',)
            return SimpleNamespace(fetchone=lambda:row)
    def scope(actual):
        assert actual is fixture
        if mode=='scope_failure':raise RuntimeError('wrong fixture')
        if mode=='scope_false':return False
    def run():return observe_fixture_learning_consent(Connection(),fixture,expected_database='isolated',
        revoked=revoked,assert_fixture_scope=scope,check_active=lambda:False if mode=='activity_false' else None)
    if mode in ('baseline','revoked'):
        result=run();assert result.revoked is revoked and result.epoch==fixture.epoch+int(revoked)
        assert result.affected_versions==(2 if revoked else 1) and events==['commit']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('scope_failure','scope_false','activity_false'):assert not queries
        else:assert events==['rollback']
    assert not any(q.startswith(('DELETE','UPDATE','INSERT')) for q in queries)
