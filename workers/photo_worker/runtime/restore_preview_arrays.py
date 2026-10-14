"""Preview source array producer/input authority around exact-version streaming."""
import psycopg
from .restore_inventory import RestoreObject,_validate
from .restore_legacy_array import verify_legacy_array_bytes
from .restore_authority import require_restore_active


def read_preview_array_object(connection,tenant_id,binding,reference,role,*,expected_database,check_active):
    if role not in ('working_rgb','working_alpha'):raise ValueError('RESTORE_PREVIEW_ARRAY_ROLE')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if scope!=(expected_database,True):raise ValueError('RESTORE_PREVIEW_ARRAY_AUTHORITY')
        row=connection.execute('''SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.artifacts root JOIN photo.tasks t ON t.tenant_id=root.tenant_id AND t.id=root.created_by_task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id
 WHERE root.tenant_id=%s AND root.id=%s AND root.sha256=%s AND root.state='ready'
 AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND root.produced_attempt=z.attempt AND root.produced_fence=z.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
 AND a.id=%s AND a.sha256=%s AND (a.output_role=%s OR (%s='working_alpha' AND a.output_role='alpha'))
 AND a.media_type='application/octet-stream'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND a.object_version NOT IN ('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND ((a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)))
 OR (%s='working_alpha' AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))))''',
            (tenant_id,binding.source_artifact_id,binding.source_sha256,
             reference.artifact_id,reference.sha256,role,role,role)).fetchone()
        if not row or len(row)!=5:raise ValueError('RESTORE_PREVIEW_ARRAY_OWNERSHIP')
        obj=RestoreObject(*row);_validate(obj)
        if obj.size>2400000128 or not obj.key.startswith('tenants/'+str(tenant_id)+'/'):
            raise ValueError('RESTORE_PREVIEW_ARRAY_SCOPE')
        require_restore_active(check_active)
    require_restore_active(check_active)
    return obj


def verify_fixture_preview_array(connection,client,buckets,tenant_id,binding,reference,shape,dtype,role,*,expected_database,check_active):
    before=read_preview_array_object(connection,tenant_id,binding,reference,role,expected_database=expected_database,check_active=check_active)
    verify_legacy_array_bytes(client,buckets,reference,before,shape,dtype,role,
        check_active=lambda:require_restore_active(check_active))
    after=read_preview_array_object(connection,tenant_id,binding,reference,role,expected_database=expected_database,check_active=check_active)
    if before!=after:raise ValueError('RESTORE_PREVIEW_ARRAY_CHANGED')
    require_restore_active(check_active)
    return before
