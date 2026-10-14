"""Operator-only fencing of restored compute leases using existing recovery.

Requires isolated database, stopped scheduler/workers and disabled outgoing IO.
Partial progress remains fenced on failure; retry before reopening any writer.
"""
from dataclasses import dataclass
import psycopg
from .restore_authority import require_restore_authority


@dataclass(frozen=True)
class RestoreLeaseEvidence:
    schema_version: str
    database: str
    expired_running: int
    expired_reservations: int
    recovered_tasks: int


def fence_restored_compute_leases(connection,*,expected_database,
                                  assert_isolated_and_paused,check_active,
                                  maximum_tasks=10_000_000):
    """Expire restored grants, then let canonical reaper increment fences.

    Each reaper call has its own transaction, preserving the existing one-task
    lock order. Reissue/retry planning remains the scheduler's responsibility.
    Other leases (mail, cleanup, download) require their own recovery stages.
    """
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)
            or type(maximum_tasks) is not int or not 1<=maximum_tasks<=10_000_000):
        raise ValueError('RESTORE_LEASE_INSTALLATION')
    def guard():
        require_restore_authority(check_active,assert_isolated_and_paused)
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
            raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    def authority():
        connection.execute("SET LOCAL statement_timeout = '30s'")
        connection.execute("SET LOCAL lock_timeout = '5s'")
        connection.execute('SET LOCAL row_security = off')
        row=connection.execute("SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)").fetchone()
        if not row or row!=(expected_database,True):
            raise ValueError('RESTORE_LEASE_DATABASE_AUTHORITY')
    guard()
    with connection.transaction():
        authority()
        total=connection.execute("SELECT count(*) FROM photo.tasks WHERE state IN ('queued','running')").fetchone()[0]
        if total>maximum_tasks:raise ValueError('RESTORE_LEASE_TASK_LIMIT')
        # Expiration alone rejects old heartbeat/commit; the canonical reaper
        # subsequently releases slots/budget, closes attempts and bumps fences.
        running=connection.execute("UPDATE photo.tasks SET lease_until='-infinity'::timestamptz WHERE state='running'").rowcount
        reservations=connection.execute("UPDATE photo.resource_reservations r SET expires_at='-infinity'::timestamptz FROM photo.tasks t WHERE r.tenant_id=t.tenant_id AND r.task_id=t.id AND r.state='reserved' AND t.state IN ('queued','running')").rowcount
        require_restore_authority(check_active,assert_isolated_and_paused)
    recovered=0
    while True:
        guard()
        with connection.transaction():
            authority()
            count=connection.execute('SELECT photo.reap_tasks(1)').fetchone()[0]
            if type(count) is not int or count not in (0,1):
                raise ValueError('RESTORE_REAPER_RECEIPT')
            recovered+=count
            if recovered>maximum_tasks:raise ValueError('RESTORE_LEASE_TASK_LIMIT')
            require_restore_authority(check_active,assert_isolated_and_paused)
        if count==0:break
    guard()
    with connection.transaction():
        authority()
        remaining=connection.execute("SELECT count(*) FROM photo.tasks WHERE state IN ('queued','running')").fetchone()[0]
        if remaining!=0:raise ValueError('RESTORE_LEASES_REMAIN')
        require_restore_authority(check_active,assert_isolated_and_paused)
    guard()
    return RestoreLeaseEvidence('restore-compute-leases/v1',expected_database,
                                running,reservations,recovered)
