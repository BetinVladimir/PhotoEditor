"""Expire restored scheduler materialization grants without changing workflows."""
from dataclasses import dataclass
import psycopg
from .restore_authority import require_restore_authority

# Explicit schema-reviewed allowlist. No request/environment-controlled tables.
_CLAIMS=(('upload_sessions','completion_lease_until'),
         ('graph_expansions','expansion_lease_until'),
         ('v30_archive_uploads','completion_lease_until'))


@dataclass(frozen=True)
class RestoreSchedulerGrantEvidence:
    schema_version: str
    database: str
    expired_claims: tuple[tuple[str,int],...]


def fence_restored_scheduler_grants(connection,*,expected_database,
                           assert_isolated_and_paused,check_active):
    """Expire completion/expansion leases, preserving paired tokens and plans.

    Canonical claims replace tokens later; restored workers cannot renew or
    commit with expired grants. No upload is completed and no graph expanded.
    """
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)):
        raise ValueError('RESTORE_SCHEDULER_GRANT_INSTALLATION')
    def guard():
        require_restore_authority(check_active,assert_isolated_and_paused)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute("SET LOCAL statement_timeout = '30s'")
        connection.execute("SET LOCAL lock_timeout = '5s'")
        connection.execute('SET LOCAL row_security = off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_SCHEDULER_GRANT_DATABASE_AUTHORITY')
        counts=[]
        for table,column in _CLAIMS:
            guard()
            count=connection.execute(
                f"UPDATE photo.{table} SET {column}='-infinity'::timestamptz WHERE {column} IS NOT NULL AND {column}>'-infinity'::timestamptz").rowcount
            if type(count) is not int or count<0:raise ValueError('RESTORE_SCHEDULER_GRANT_RECEIPT')
            counts.append((table,count))
        for table,column in _CLAIMS:
            guard()
            remaining=connection.execute(
                f"SELECT count(*) FROM photo.{table} WHERE {column} IS NOT NULL AND {column}>'-infinity'::timestamptz").fetchone()
            if remaining!=(0,):raise ValueError('RESTORE_SCHEDULER_GRANT_CLAIMS_REMAIN')
        guard()
    guard()
    return RestoreSchedulerGrantEvidence('restore-scheduler-grants/v1',expected_database,tuple(counts))
