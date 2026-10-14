"""Observe adopted legacy Apply output without claiming physical image quality."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from .restore_inventory import RestoreObject,_validate
from .restore_legacy_catalogue import observe_fixture_legacy_apply_registration
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacyApplyResult:
    registration: object
    item_id: UUID
    asset_version_id: UUID
    artifact_id: UUID
    object: RestoreObject


def observe_fixture_legacy_apply_result(connection,target,binding,ticket,*,expected_database,
                                        assert_fixture_scope,check_active):
    registration=observe_fixture_legacy_apply_registration(connection,target,binding,ticket,
        expected_database=expected_database,assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,ticket)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_LEGACY_RESULT_AUTHORITY')
        row=connection.execute('''SELECT i.id,v.id,a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.runs r JOIN photo.apply_builds b ON b.tenant_id=r.tenant_id AND b.run_id=r.id
 JOIN photo.collection_items i ON i.tenant_id=r.tenant_id AND i.collection_id=r.output_collection_id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id AND t.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 WHERE r.tenant_id=%s AND r.id=%s AND r.output_collection_id=%s AND r.kind='apply'
 AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
 AND b.phase='complete' AND b.item_count=1 AND b.lease_token IS NULL AND b.lease_until IS NULL AND b.last_error IS NULL
 AND i.source_item_id=%s AND i.created_by_run_id=r.id AND v.created_by_run_id=r.id
 AND v.parent_asset_version_id=%s AND v.id<>v.parent_asset_version_id
 AND EXISTS(SELECT FROM photo.asset_version_sources src WHERE src.tenant_id=v.tenant_id AND src.version_id=v.id
 AND src.source_version_id=v.parent_asset_version_id AND src.role='apply_source' AND src.ordinal=0)
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='working_image'
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (SELECT count(*) FROM photo.collection_items n WHERE n.tenant_id=r.tenant_id AND n.collection_id=r.output_collection_id)=1''',
            (target.tenant_id,ticket.run_id,ticket.output_collection_id,target.item_id,binding.asset_version_id)).fetchone()
        if (not row or len(row)!=8 or any(not isinstance(v,UUID) or v.int==0 for v in row[:3])):
            raise ValueError('RESTORE_LEGACY_RESULT_NOT_READY')
        item,version,artifact,*physical=row
        obj=RestoreObject(*physical);_validate(obj)
        if not obj.key.startswith('tenants/'+str(target.tenant_id)+'/'):
            raise ValueError('RESTORE_LEGACY_RESULT_SCOPE')
        guard()
    guard();return RestoreLegacyApplyResult(registration,item,version,artifact,obj)
