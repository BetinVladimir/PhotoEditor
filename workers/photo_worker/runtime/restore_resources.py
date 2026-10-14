"""Release restored reservations through canonical accounting; clear stale slots."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from .restore_authority import require_restore_authority


@dataclass(frozen=True)
class RestoreResourceEvidence:
    schema_version: str
    database: str
    released_reservations: int
    cleared_slots: int


def recover_restored_resources(connection,*,expected_database,
                                assert_isolated_and_paused,check_active,
                                maximum_reservations=10_000_000):
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)
            or type(maximum_reservations) is not int or not 1<=maximum_reservations<=10_000_000):
        raise ValueError('RESTORE_RESOURCE_INSTALLATION')
    def guard():
        require_restore_authority(check_active,assert_isolated_and_paused)
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
            raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    def authority():
        connection.execute("SET LOCAL statement_timeout = '30s'")
        connection.execute("SET LOCAL lock_timeout = '5s'")
        connection.execute('SET LOCAL row_security = off')
        row=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if row!=(expected_database,True):raise ValueError('RESTORE_RESOURCE_DATABASE_AUTHORITY')
        active=connection.execute("SELECT EXISTS(SELECT FROM photo.tasks WHERE state IN ('queued','running'))").fetchone()
        if active!=(False,):raise ValueError('RESTORE_RESOURCE_ACTIVE_TASKS')
    def before_commit():
        require_restore_authority(check_active,assert_isolated_and_paused)
    released=0
    while True:
        guard()
        with connection.transaction():
            authority()
            row=connection.execute("SELECT tenant_id,task_id,fencing_token FROM photo.resource_reservations WHERE state='reserved' ORDER BY tenant_id,task_id LIMIT 1").fetchone()
            if row is not None:
                if (len(row)!=3 or not all(isinstance(v,UUID) and v.int for v in row[:2])
                        or type(row[2]) is not int or row[2]<0):raise ValueError('RESTORE_RESOURCE_RESERVATION')
                if released>=maximum_reservations:raise ValueError('RESTORE_RESOURCE_LIMIT')
                tenant,task,fence=row
                locked=connection.execute('SELECT id FROM photo.lock_task(%s)',(task,)).fetchone()
                if locked!=(task,):raise ValueError('RESTORE_RESOURCE_TASK_MISSING')
                current=connection.execute("SELECT fencing_token FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s AND state='reserved' FOR UPDATE",(tenant,task)).fetchone()
                if current!=(fence,):raise ValueError('RESTORE_RESOURCE_AUTHORITY_CHANGED')
                connection.execute('SELECT photo.release_task_resources(%s,%s,%s)',(tenant,task,fence))
                remaining=connection.execute("SELECT EXISTS(SELECT FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s AND state='reserved')",(tenant,task)).fetchone()
                if remaining!=(False,):raise ValueError('RESTORE_RESOURCE_RELEASE_FAILED')
                released+=1
            before_commit()
        if row is None:break
    guard()
    with connection.transaction():
        authority()
        # No active tasks/reservations now exist. Clear stale capacity bindings,
        # preserving slot capacity/node identity and canonical GPU accounting.
        slots=connection.execute('UPDATE photo.resource_slots SET tenant_id=NULL,task_id=NULL,fencing_token=NULL,lease_until=NULL WHERE tenant_id IS NOT NULL OR task_id IS NOT NULL OR fencing_token IS NOT NULL OR lease_until IS NOT NULL').rowcount
        if type(slots) is not int or slots<0:raise ValueError('RESTORE_RESOURCE_SLOT_RECEIPT')
        inconsistent=connection.execute('SELECT EXISTS(SELECT FROM photo.tenant_usage WHERE active_gpu_tasks<>0)').fetchone()
        if inconsistent!=(False,):raise ValueError('RESTORE_RESOURCE_ACCOUNTING_INCONSISTENT')
        # A backup's worker liveness/free-capacity observations are not current
        # authority. Preserve operator pins, qualifications, enabled flags and
        # budgets; require new authenticated measurements after controlled resume.
        workers=connection.execute("UPDATE photo.worker_registrations SET last_seen='-infinity'::timestamptz,reported_capacity_policy_sha=NULL,capacity_policy_reported_at=NULL").rowcount
        if type(workers) is not int or workers<0:raise ValueError('RESTORE_RESOURCE_WORKER_RECEIPT')
        nodes=connection.execute('UPDATE photo.resource_nodes SET capacity_report_worker_id=NULL,capacity_report_policy_sha=NULL,reported_at=NULL,ram_available_bytes=0,vram_available_bytes=0,scratch_available_bytes=0').rowcount
        if type(nodes) is not int or nodes<0:raise ValueError('RESTORE_RESOURCE_CAPACITY_RECEIPT')
        before_commit()
    guard()
    return RestoreResourceEvidence('restore-resource-recovery/v1',expected_database,released,slots)
