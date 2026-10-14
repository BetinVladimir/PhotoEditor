"""Final read-only recovery invariant check across all implemented grant families."""
import psycopg
from .restore_cleanup import _CLAIMS as _CLEANUP
from .restore_scheduler_grants import _CLAIMS as _SCHEDULER

_PREDICATES=(
    ('tasks',"state IN ('queued','running')"),
    ('resource_reservations',"state='reserved'"),
    ('resource_slots','tenant_id IS NOT NULL OR task_id IS NOT NULL OR fencing_token IS NOT NULL OR lease_until IS NOT NULL'),
    ('tenant_usage','active_gpu_tasks<>0'),
    ('worker_registrations',"last_seen IS DISTINCT FROM '-infinity'::timestamptz OR reported_capacity_policy_sha IS NOT NULL OR capacity_policy_reported_at IS NOT NULL"),
    ('resource_nodes','capacity_report_worker_id IS NOT NULL OR capacity_report_policy_sha IS NOT NULL OR reported_at IS NOT NULL OR ram_available_bytes<>0 OR vram_available_bytes<>0 OR scratch_available_bytes<>0'),
    ('artifacts a',"a.state='ready' AND EXISTS(SELECT FROM photo.restore_quarantine_versions q WHERE q.bucket=a.bucket AND q.object_key=a.object_key AND q.object_version=a.object_version)"),
    ('mail_payloads',"status='sending'"),
    ('outbox','publish_lease_until IS NOT NULL'),
    ('auth_mail_outbox','publish_lease_until IS NOT NULL'),
    ('download_intents','consumed_at IS NULL AND expires_at<=transaction_timestamp()'),
)+tuple((table,f"{column} IS NOT NULL AND {column}>'-infinity'::timestamptz")
        for table,column in (*_CLEANUP,*_SCHEDULER))


def verify_recovery_fences(connection,*,expected_database,
                           assert_isolated_and_paused,check_active):
    """Reject any revived grant using one complete privileged SQL snapshot.

    This checks implemented recovery families, not every product authority or
    permission to resume writers. Isolation must remain externally enforced.
    """
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)):
        raise ValueError('RESTORE_FINAL_FENCE_INSTALLATION')
    def guard():
        if check_active() is False:
            raise RuntimeError('RESTORE_FINAL_FENCE_INACTIVE')
        if assert_isolated_and_paused() is False:
            raise RuntimeError('RESTORE_WRITERS_NOT_PAUSED')
        if check_active() is False:
            raise RuntimeError('RESTORE_FINAL_FENCE_INACTIVE')
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute('SET LOCAL row_security = off')
        connection.execute("SET LOCAL statement_timeout = '30s'")
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_FINAL_FENCE_DATABASE_AUTHORITY')
        for table,predicate in _PREDICATES:
            guard()
            row=connection.execute(f'SELECT EXISTS(SELECT FROM photo.{table} WHERE {predicate})').fetchone()
            if row!=(False,):raise ValueError('RESTORE_RECOVERY_GRANT_REAPPEARED')
        guard()
    guard()
