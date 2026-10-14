"""Canonical encoded item of a ready one-item download job; volume separate."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from .restore_authority import require_restore_active
from .restore_inventory import RestoreObject,_validate
from .restore_export_catalogue import observe_fixture_export_registration


@dataclass(frozen=True)
class RestoreEncodedExport:
    registration: object
    artifact_id: UUID
    object: RestoreObject
    media_type: str
    entry_name: str
    volume_no: int
    delivery_kind: str


def observe_fixture_encoded_export(connection,target,binding,ticket,selection,asset_version_id,*,expected_database,check_active):
    require_restore_active(check_active)
    registration=observe_fixture_export_registration(connection,target,binding,ticket,selection,
        expected_database=expected_database,check_active=check_active)
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_EXPORT_RESULT_AUTHORITY')
        row=connection.execute('''SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,
 a.media_type,i.entry_name,i.volume_no,i.delivery_kind
 FROM photo.download_jobs j JOIN photo.runs r ON r.tenant_id=j.tenant_id AND r.id=j.run_id
 JOIN photo.download_job_items i ON i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.ordinal=0
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 AND a.bytes=i.bytes
 JOIN photo.download_export_plans p ON p.tenant_id=j.tenant_id AND p.run_id=r.id AND p.ordinal=i.ordinal
 JOIN photo.snapshot_items si ON si.tenant_id=j.tenant_id AND si.snapshot_id=j.snapshot_id AND si.ordinal=i.ordinal
 JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=r.id AND t.id=a.created_by_task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 WHERE j.tenant_id=%s AND j.id=%s AND r.id=%s AND j.status='ready' AND NOT j.cancel_requested
 AND j.expires_at>clock_timestamp() AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
 AND i.asset_version_id=%s AND i.volume_no=1 AND i.delivery_kind IN ('single','zip')
 AND si.asset_version_id=i.asset_version_id
 AND p.params->'image'=jsonb_build_object('artifact_id',si.artifact_id,'sha256',si.sha256)
 AND p.inputs->0=p.params->'image' AND t.inputs=p.inputs
 AND (p.params-'image'-'icc_sha256')=j.preset->'config'
 AND p.params->>'icc_sha256' ~ '^[0-9a-f]{64}$'
 AND a.output_role='encoded_image'
 AND t.operation='export.encode'  AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type IN ('image/jpeg','image/png','image/tiff')
 AND a.bytes BETWEEN 1 AND 1073741824 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (SELECT count(*) FROM photo.download_job_items n WHERE n.tenant_id=j.tenant_id AND n.download_job_id=j.id)=1''',
            (target.tenant_id,ticket.download_job_id,registration.run_id,asset_version_id)).fetchone()
        if not row or len(row)!=10 or not isinstance(row[0],UUID) or row[0].int==0:
            raise ValueError('RESTORE_EXPORT_RESULT_NOT_READY')
        artifact,*rest=row;obj=RestoreObject(*rest[:5]);_validate(obj)
        media,name,volume,kind=rest[5:]
        if (not obj.key.startswith('tenants/'+str(target.tenant_id)+'/') or not isinstance(name,str)
                or not 1<=len(name.encode())<=180 or any(c in name for c in ('/','\\'))
                or any(ord(c)<32 or ord(c)==127 for c in name) or name in ('.','..')):
            raise ValueError('RESTORE_EXPORT_RESULT_SCOPE')
        require_restore_active(check_active)
    return RestoreEncodedExport(registration,artifact,obj,media,name,volume,kind)
