"""Canonical quality task and exact physical legacy master report."""
from dataclasses import dataclass
from hashlib import sha256
import psycopg
from photo_worker.contracts.base import ArtifactRef
from .restore_inventory import RestoreObject,_validate
from .restore_objects import MinioRestoreObjects
from .restore_legacy_quality import validate_legacy_master_quality
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreLegacyPhysicalQuality:
    object: RestoreObject
    semantics: object


def read_legacy_quality_object(connection,tenant_id,result,*,check_active):
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(result.registration.database,True):raise ValueError('RESTORE_LEGACY_QUALITY_AUTHORITY')
        rows=connection.execute('''SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.run_id=n.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
 WHERE n.tenant_id=%s AND n.run_id=%s AND n.phase='quality' AND n.ordinal=0
 AND t.operation='quality.check' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND a.output_role='quality_report' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND n.params->'target'=jsonb_build_object('artifact_id',%s::uuid,'sha256',%s::text)
 AND n.params->>'target_kind'='working' AND n.params->>'stage'='master'
 AND n.params->>'threshold_profile_id'='master_v1'
 AND t.inputs @> jsonb_build_array(n.params->'target') LIMIT 2''',
            (tenant_id,result.registration.run_id,result.artifact_id,result.object.sha256)).fetchall()
        if len(rows)!=1 or len(rows[0])!=5:raise ValueError('RESTORE_LEGACY_QUALITY_PRODUCER')
        obj=RestoreObject(*rows[0]);_validate(obj)
        if not obj.key.startswith('tenants/'+str(tenant_id)+'/'):raise ValueError('RESTORE_LEGACY_QUALITY_SCOPE')
        require_restore_active(check_active)
    require_restore_active(check_active)
    return obj


def verify_fixture_legacy_quality(connection,client,buckets,tenant_id,result,*,check_active):
    before=read_legacy_quality_object(connection,tenant_id,result,check_active=check_active)
    require_restore_active(check_active)
    store=MinioRestoreObjects(client,buckets,check_active=lambda:require_restore_active(check_active))
    with store.open_version(before) as stream:
        chunks=[];size=0
        while True:
            block=stream.read(min(65536,before.size-size+1))
            if not block:break
            size+=len(block)
            if size>before.size:raise ValueError('RESTORE_LEGACY_QUALITY_SIZE')
            chunks.append(block)
    raw=b''.join(chunks)
    if size!=before.size or sha256(raw).hexdigest()!=before.sha256:raise ValueError('RESTORE_LEGACY_QUALITY_SHA')
    semantics=validate_legacy_master_quality(raw,ArtifactRef(artifact_id=result.artifact_id,sha256=result.object.sha256),
        check_active=lambda:require_restore_active(check_active))
    if read_legacy_quality_object(connection,tenant_id,result,check_active=check_active)!=before:
        raise ValueError('RESTORE_LEGACY_QUALITY_CHANGED')
    require_restore_active(check_active);return RestoreLegacyPhysicalQuality(before,semantics)
