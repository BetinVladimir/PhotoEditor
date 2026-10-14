from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_fence_check import verify_recovery_fences


@pytest.mark.parametrize('revived',['none','quarantined_ready','tasks','resource_reservations','resource_slots','tenant_usage','worker_registrations','resource_nodes','mail_payloads','outbox','auth_mail_outbox',
    'download_intents','output_uploads','backend_documents','ingest_originals','deletion_jobs',
    'staging_cleanup','upload_abort_cleanup','artifacts','upload_sessions','graph_expansions','archive_completion','archive_cleanup'])
def test_final_snapshot_refuses_each_revived_grant_family(revived):
    events=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            finally:events.append('end')
        def execute(self,sql):
            events.append(sql)
            if sql.startswith('SELECT current_database'):row=('isolated',True)
            else:row=(f'FROM photo.{revived} WHERE' in sql or
                (revived=='quarantined_ready' and 'restore_quarantine_versions' in sql) or
                (revived=='archive_completion' and 'FROM photo.v30_archive_uploads WHERE completion_lease_until' in sql) or
                (revived=='archive_cleanup' and 'FROM photo.v30_archive_uploads WHERE cleanup_lease_until' in sql),)
            return SimpleNamespace(fetchone=lambda:row)
    def run():return verify_recovery_fences(Connection(),expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=lambda:None)
    if revived=='none':
        run();assert events.count('begin')==1
        assert len([e for e in events if e.startswith('SELECT EXISTS')])==22
    else:
        with pytest.raises(ValueError):run()
    assert events[-1]=='end'
    assert not any(e.startswith('UPDATE') for e in events)


@pytest.mark.parametrize('denial',['active_before','isolation_before','active_after','isolation_mid'])
def test_final_snapshot_refuses_explicit_false_at_guard_boundaries(denial):
    events=[];checks={'active':0,'isolation':0}
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            finally:events.append('end')
        def execute(self,sql):
            events.append(sql)
            return SimpleNamespace(fetchone=lambda:('isolated',True) if sql.startswith('SELECT current_database') else (False,))
    def active():
        checks['active']+=1
        return False if denial=='active_before' and checks['active']==1 or denial=='active_after' and checks['active']==2 else None
    def isolated():
        checks['isolation']+=1
        return False if denial=='isolation_before' and checks['isolation']==1 or denial=='isolation_mid' and checks['isolation']==2 else None
    with pytest.raises(RuntimeError):
        verify_recovery_fences(Connection(),expected_database='isolated',
            assert_isolated_and_paused=isolated,check_active=active)
    if denial.endswith('before') or denial=='active_after':
        assert events==[]
    else:
        assert events[-1]=='end'
        assert not any(item.startswith('SELECT EXISTS') for item in events)
