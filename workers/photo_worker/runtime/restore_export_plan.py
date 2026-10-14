"""Bind physical verifier EncodeParams to exact registered canonical task plan."""
from hashlib import sha256
import psycopg
from .restore_authority import require_restore_active
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.export import EncodeParams
from .restore_inventory import RestoreObject,_validate
from .restore_objects import MinioRestoreObjects


def verify_fixture_export_plan(connection,tenant_id,result,params,*,expected_database,check_active,_physical=False):
    if not isinstance(params,EncodeParams) or not callable(check_active):
        raise ValueError('RESTORE_EXPORT_PLAN_INSTALLATION')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_EXPORT_PLAN_AUTHORITY')
        row=connection.execute('''SELECT p.params,p.params_sha,t.params,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes
 FROM photo.download_export_plans p JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.run_id=p.run_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(t.params->>'artifact_id')::uuid
 JOIN photo.artifacts encoded ON encoded.tenant_id=t.tenant_id AND encoded.created_by_task_id=t.id
 WHERE p.tenant_id=%s AND p.run_id=%s AND p.ordinal=0 AND p.operation='export.encode'
 AND t.operation='export.encode' AND t.operation_version='1' AND t.inputs=p.inputs
 AND encoded.id=%s AND encoded.sha256=%s AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.sha256=p.params_sha AND t.params->>'sha256'=p.params_sha''',
            (tenant_id,result.registration.run_id,result.artifact_id,result.object.sha256)).fetchone()
        if not row or len(row)!=8 or not isinstance(row[0],dict) or not isinstance(row[2],dict):
            raise ValueError('RESTORE_EXPORT_PLAN_MISSING')
        document,digest,task_ref,artifact_digest,bucket,key,version,size=row
        raw=canonical_bytes(document)
        actual=EncodeParams.model_validate_json(raw)
        # Go omits nullable optional fields. Preserve that exact canonical
        # representation while comparing full strict typed execution semantics.
        if (actual!=params or sha256(raw).hexdigest()!=digest or artifact_digest!=digest
                or set(task_ref)!={'artifact_id','sha256'} or task_ref['sha256']!=digest
                or parse_json(raw)!=params.model_dump(mode='json',exclude_none=True)):
            raise ValueError('RESTORE_EXPORT_PLAN_BINDING')
        require_restore_active(check_active)
    obj=RestoreObject(bucket,key,version,digest,size);_validate(obj)
    if not obj.key.startswith('tenants/'+str(tenant_id)+'/'):
        raise ValueError('RESTORE_EXPORT_PLAN_SCOPE')
    return (digest,obj,raw) if _physical else digest



def verify_fixture_export_params(connection,client,buckets,tenant_id,result,params,*,expected_database,check_active):
    require_restore_active(check_active)
    def observe():return verify_fixture_export_plan(connection,tenant_id,result,params,
        expected_database=expected_database,check_active=check_active,_physical=True)
    before=observe();digest,obj,expected_raw=before
    if not 2<=obj.size<=1048576:raise ValueError('RESTORE_EXPORT_PARAMS_SIZE')
    store=MinioRestoreObjects(client,buckets,check_active=check_active)
    with store.open_version(obj) as stream:
        chunks=[];size=0
        while True:
            require_restore_active(check_active);block=stream.read(min(65536,obj.size-size+1))
            if not block:break
            size+=len(block)
            if size>obj.size:raise ValueError('RESTORE_EXPORT_PARAMS_SIZE')
            chunks.append(block)
    raw=b''.join(chunks)
    if len(raw)!=obj.size or sha256(raw).hexdigest()!=digest or raw!=expected_raw:
        raise ValueError('RESTORE_EXPORT_PARAMS_BYTES')
    if observe()!=before:raise ValueError('RESTORE_EXPORT_PARAMS_CHANGED')
    require_restore_active(check_active);return digest
