"""Read-only PostgreSQL adapter for independent restore deletion history."""
from dataclasses import dataclass
from datetime import datetime
import psycopg
from .restore_deletions import RestoreDeletionEvidence, verify_restore_deletions
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreDatabaseDeletionEvidence:
    schema_version: str
    database: str
    snapshot: str
    observed_at: datetime
    history: RestoreDeletionEvidence


def verify_restore_database_deletions(connection,entries,checkpoint,*,
                                       expected_database,check_active,
                                       maximum_entries=10_000_000):
    """Verify only; a resurrected tenant requires separate fenced remediation.

    The caller provides the independent latest checkpoint and event stream.
    Neither is derived from this restored database. The explicit database name
    prevents accidental default-database selection but is not cluster isolation
    attestation; the operator must hold writers/outgoing disabled externally.
    """
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or any(ord(c)<32 for c in expected_database) or not callable(check_active)):
        raise ValueError('RESTORE_DELETION_DATABASE_INSTALLATION')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute('SET LOCAL row_security = off')
        row=connection.execute("""SELECT current_database(),pg_current_snapshot()::text,
 transaction_timestamp(),(SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user)
""").fetchone()
        if (not row or len(row)!=4 or row[0]!=expected_database
                or not isinstance(row[1],str) or not row[1]
                or not isinstance(row[2],datetime) or row[2].tzinfo is None or row[3] is not True):
            raise ValueError('RESTORE_DELETION_DATABASE_AUTHORITY')
        def status(tenant):
            require_restore_active(check_active)
            value=connection.execute('SELECT status FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()
            require_restore_active(check_active)
            if value is None:return None
            if len(value)!=1 or not isinstance(value[0],str):
                raise ValueError('RESTORE_TENANT_STATE_INVALID')
            return value[0]
        history=verify_restore_deletions(entries,checkpoint,status,
            check_active=check_active,maximum_entries=maximum_entries)
        require_restore_active(check_active)
        evidence=RestoreDatabaseDeletionEvidence('restore-database-deletions/v1',
            row[0],row[1],row[2],history)
    require_restore_active(check_active)
    return evidence
