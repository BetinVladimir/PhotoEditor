"""Bind restore codec admission to the canonical export task CPU/RAM profile."""
from hashlib import sha256
import psycopg
from .restore_authority import require_restore_active
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.export.memory import admit_presentation


def verify_fixture_export_memory(connection,tenant_id,result,source,width,height,*,expected_database,check_active):
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_EXPORT_MEMORY_AUTHORITY')
        row=connection.execute('''SELECT t.execution_profile
 FROM photo.download_export_plans p JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
 WHERE p.tenant_id=%s AND p.run_id=%s AND p.ordinal=0 AND p.operation='export.encode'
 AND t.operation='export.encode' AND t.operation_version='1' AND t.state='succeeded'
 AND t.resource_class='cpu' AND NOT t.cancel_requested AND t.inputs=p.inputs
 AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND a.id=%s AND a.sha256=%s AND a.output_role='encoded_image'
 AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))''',
            (tenant_id,result.registration.run_id,result.artifact_id,result.object.sha256)).fetchone()
        if not row or len(row)!=1 or not isinstance(row[0],dict) or row[0].get('backend')!='cpu':
            raise ValueError('RESTORE_EXPORT_MEMORY_PROFILE')
        profile=row[0]
        # Production's conservative presentation estimate also bounds codec
        # verification. This is admission, not an OS memory reservation.
        admit_presentation(profile,source.width,source.height,width,height,alpha=source.alpha is not None)
        digest=sha256(canonical_bytes(profile)).hexdigest();require_restore_active(check_active)
    return digest
