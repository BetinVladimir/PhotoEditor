"""Independent read-only SQL observation of a completed fixture tenant purge."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from psycopg import sql
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestorePurgeMetadataEvidence:
    schema_version: str
    database: str
    tenant_id: UUID
    deletion_job_id: UUID
    checked_tables: tuple[str, ...]


def verify_purged_tenant_metadata(connection, tenant_id, deletion_job_id, *,
                                 expected_database, assert_fixture_scope, check_active):
    """No cleanup or repair. Unexpected surviving tenant-scoped rows refuse.

    Tombstone, suspended memberships, tenant lifecycle audit and deletion jobs
    are deliberately retained by canonical finish_tenant_deletion. All other
    current photo tables containing tenant_id must be empty for this fixture.
    """
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(deletion_job_id,UUID) or deletion_job_id.int==0
            or not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_PURGE_PROBE_INSTALLATION')
    def guard():
        require_restore_active(check_active)
        if assert_fixture_scope(tenant_id,deletion_job_id) is False:
            raise RuntimeError('RESTORE_PURGE_FIXTURE_DENIED')
        require_restore_active(check_active)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='30s'")
        connection.execute("SET LOCAL lock_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        row=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if row!=(expected_database,True):raise ValueError('RESTORE_PURGE_PROBE_AUTHORITY')
        row=connection.execute('''SELECT
 (SELECT status FROM photo.tenants WHERE id=%s),
 EXISTS(SELECT FROM photo.deletion_jobs WHERE id=%s AND tenant_id=%s
   AND resource_type='tenant' AND status='completed' AND cleanup_token IS NULL AND cleanup_lease_until IS NULL),
 EXISTS(SELECT FROM photo.memberships WHERE tenant_id=%s AND status<>'suspended'),
 EXISTS(SELECT FROM photo.audit_log WHERE tenant_id=%s AND action NOT LIKE 'tenant.%%')''',
            (tenant_id,deletion_job_id,tenant_id,tenant_id,tenant_id)).fetchone()
        if row!=('deleted',True,False,False):raise ValueError('RESTORE_PURGE_PROBE_TOMBSTONE')
        guard()
        tables=connection.execute('''SELECT c.relname FROM pg_catalog.pg_class c
 JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 WHERE n.nspname='photo' AND c.relkind IN ('r','p') AND NOT c.relispartition
 AND a.attname='tenant_id' AND a.attnum>0 AND NOT a.attisdropped
 ORDER BY c.relname COLLATE "C" LIMIT 513''').fetchall()
        names=tuple(row[0] for row in tables)
        if (not 1<=len(names)<=512 or len(set(names))!=len(names)
                or not {'artifacts','tasks','outbox','consent_grants'}<=set(names)):
            raise ValueError('RESTORE_PURGE_PROBE_CATALOGUE')
        checked=[]
        for name in names:
            guard()
            if name in ('memberships','audit_log','deletion_jobs'):continue
            row=connection.execute(sql.SQL('SELECT EXISTS(SELECT FROM photo.{} WHERE tenant_id=%s)').format(sql.Identifier(name)),(tenant_id,)).fetchone()
            if row!=(False,):raise ValueError('RESTORE_PURGE_PROBE_METADATA_REMAINS')
            checked.append(name)
        guard()
    guard()
    return RestorePurgeMetadataEvidence('restore-purge-metadata/v1',expected_database,
        tenant_id,deletion_job_id,tuple(checked))
