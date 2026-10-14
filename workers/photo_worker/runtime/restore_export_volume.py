"""Ready one-item delivery volume with canonical archive producer authority."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from .restore_authority import require_restore_active
from .restore_inventory import RestoreObject,_validate


@dataclass(frozen=True)
class RestoreExportVolume:
    artifact_id: UUID
    object: RestoreObject
    manifest_id: UUID|None
    manifest_object: RestoreObject|None
    delivery_kind: str


def read_fixture_export_volume(connection,tenant_id,encoded,*,expected_database,check_active):
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_EXPORT_VOLUME_AUTHORITY')
        row=connection.execute('''SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,
 m.id,m.bucket,m.object_key,m.object_version,m.sha256,m.bytes
 FROM photo.download_jobs j JOIN photo.download_volumes v ON v.tenant_id=j.tenant_id AND v.download_job_id=j.id
 JOIN photo.download_job_items i ON i.tenant_id=j.tenant_id AND i.download_job_id=j.id AND i.volume_no=v.volume_no
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.artifact_id
 LEFT JOIN photo.artifacts m ON m.tenant_id=v.tenant_id AND m.id=v.manifest_artifact_id
 LEFT JOIN photo.download_archive_plans p ON p.tenant_id=j.tenant_id AND p.run_id=j.run_id AND p.volume_no=v.volume_no
 LEFT JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id
 LEFT JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 WHERE j.tenant_id=%s AND j.id=%s AND j.run_id=%s AND j.status='ready' AND NOT j.cancel_requested
 AND j.expires_at>clock_timestamp() AND v.volume_no=%s AND v.status='ready' AND v.files_done=1
 AND v.bytes_written=a.bytes AND i.ordinal=0 AND i.artifact_id=%s AND i.sha256=%s AND i.delivery_kind=%s
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND a.object_version NOT IN ('','null') AND a.expires_at>clock_timestamp()
 AND ((i.delivery_kind='single' AND a.id=i.artifact_id AND m.id IS NULL)
 OR (i.delivery_kind='zip' AND a.output_role='archive' AND a.media_type='application/zip'
 AND t.operation='archive.build' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND m.created_by_task_id=t.id AND m.produced_attempt=z.attempt AND m.produced_fence=z.fencing_token
 AND m.output_role='archive_manifest' AND m.media_type='application/json' AND m.bytes BETWEEN 2 AND 1048576
 AND m.state='ready' AND m.verified_at IS NOT NULL AND m.object_version IS NOT NULL
 AND m.object_version NOT IN ('','null') AND m.expires_at>clock_timestamp()
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256),
 jsonb_build_object('artifact_id',m.id,'sha256',m.sha256))))
 AND (SELECT count(*) FROM photo.download_volumes n WHERE n.tenant_id=j.tenant_id AND n.download_job_id=j.id)=1
 AND (SELECT count(*) FROM photo.download_job_items n WHERE n.tenant_id=j.tenant_id AND n.download_job_id=j.id)=1''',
            (tenant_id,encoded.registration.download_job_id,encoded.registration.run_id,encoded.volume_no,
             encoded.artifact_id,encoded.object.sha256,encoded.delivery_kind)).fetchone()
        if not row or len(row)!=12 or not isinstance(row[0],UUID) or row[0].int==0:
            raise ValueError('RESTORE_EXPORT_VOLUME_NOT_READY')
        artifact=row[0];obj=RestoreObject(*row[1:6]);_validate(obj)
        manifest_id=row[6];manifest=None
        if encoded.delivery_kind=='zip':
            if not isinstance(manifest_id,UUID) or manifest_id.int==0 or manifest_id==artifact:
                raise ValueError('RESTORE_EXPORT_VOLUME_MANIFEST')
            manifest=RestoreObject(*row[7:]);_validate(manifest)
        elif encoded.delivery_kind!='single' or any(v is not None for v in row[6:]) or artifact!=encoded.artifact_id or obj!=encoded.object:
            raise ValueError('RESTORE_EXPORT_VOLUME_SINGLE')
        for value in (obj,manifest):
            if value is not None and (not value.key.startswith('tenants/'+str(tenant_id)+'/') or not 1<=value.size<=10737418240):
                raise ValueError('RESTORE_EXPORT_VOLUME_SCOPE')
        require_restore_active(check_active)
    return RestoreExportVolume(artifact,obj,manifest_id,manifest,encoded.delivery_kind)
