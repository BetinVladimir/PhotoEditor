"""Fresh native selection for the exact adopted Apply output; no replay retry."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from .restore_selection import RestoreSelectionTarget,validate_fixture_selection,request_fixture_selection
from .restore_selection_catalogue import observe_fixture_selection
from .restore_legacy_result import RestoreLegacyApplyResult
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRegisteredSelection:
    ticket: object
    registration: object


def create_fixture_selection(connection,target,actor_id,applied,*,expected_database,tls_context,
        assert_installed_fixture,check_active):
    if (not isinstance(target,RestoreSelectionTarget) or not isinstance(applied,RestoreLegacyApplyResult)
            or not isinstance(actor_id,UUID) or actor_id.int==0
            or target.collection_id!=applied.registration.output_collection_id or target.item_id!=applied.item_id
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_SELECTION_LIFECYCLE_INSTALLATION')
    validate_fixture_selection(target,tls_context=tls_context,assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_installed_fixture)
    def scope(*_):guard()
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_SELECTION_LIFECYCLE_AUTHORITY')
        exists=connection.execute('''SELECT EXISTS(SELECT FROM photo.idempotency_records
 WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s AND route='Selection.Create' AND key=%s)''',
            (target.tenant_id,actor_id,target.idempotency_key)).fetchone()
        if exists!=(False,):raise ValueError('RESTORE_SELECTION_ALREADY_REQUESTED')
        ready=connection.execute('''SELECT EXISTS(SELECT FROM photo.collections c
 JOIN photo.collection_items i ON i.tenant_id=c.tenant_id AND i.collection_id=c.id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id
 JOIN photo.assets source ON source.tenant_id=v.tenant_id AND source.id=v.asset_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 WHERE c.tenant_id=%s AND c.id=%s AND c.revision=%s AND c.source_run_id=%s
 AND c.deleted_at IS NULL AND c.state<>'archived' AND i.id=%s AND i.asset_version_id=%s
 AND a.id=%s AND a.sha256=%s AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND source.deleted_at IS NULL AND source.upload_state='verified')''',
            (target.tenant_id,target.collection_id,target.collection_revision,applied.registration.run_id,
             applied.item_id,applied.asset_version_id,applied.artifact_id,applied.object.sha256)).fetchone()
        if ready!=(True,):raise ValueError('RESTORE_SELECTION_SOURCE_CHANGED')
        guard()
    # The fixture owns this key exclusively; readonly preflight is not a lock.
    guard();ticket=request_fixture_selection(target,tls_context=tls_context,assert_fixture_scope=scope,check_active=guard)
    guard();registration=observe_fixture_selection(connection,target,ticket,actor_id,applied,
        expected_database=expected_database,check_active=guard)
    guard();return RestoreRegisteredSelection(ticket,registration)
