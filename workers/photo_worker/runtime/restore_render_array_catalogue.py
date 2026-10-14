"""Canonical produced NPY authority, separate from source-array reuse."""
from uuid import UUID
import psycopg
from photo_worker.contracts.base import ArtifactRef
from .restore_inventory import RestoreObject,_validate
from .restore_batch_result import RestoreBatchResultEvidence
from .restore_render_array import verify_restore_render_array
from .restore_authority import require_restore_fixture


def read_fixture_produced_array(connection,tenant_id,result,reference,role,*,assert_fixture_scope,check_active):
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0 or not isinstance(result,RestoreBatchResultEvidence)
            or not isinstance(reference,ArtifactRef) or reference.artifact_id.int==0
            or role not in ('pixels','alpha','mask_pixels')
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_ARRAY_CATALOGUE_INSTALLATION')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,tenant_id,result,reference,role)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if scope!=(result.database,True):raise ValueError('RESTORE_ARRAY_CATALOGUE_AUTHORITY')
        row=connection.execute('''SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 WHERE a.tenant_id=%s AND a.id=%s AND a.sha256=%s AND a.output_role=%s AND a.media_type='application/x-npy'
 AND t.id=%s AND t.state='succeeded' AND t.operation='render.compose' AND t.operation_version='1'
 AND z.id=%s AND t.attempt=%s AND t.fencing_token=%s AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())''',
            (tenant_id,reference.artifact_id,reference.sha256,role,result.task_id,result.result_id,result.attempt,result.fence)).fetchone()
        if not row or len(row)!=5:raise ValueError('RESTORE_ARRAY_CATALOGUE_PRODUCER')
        obj=RestoreObject(*row);_validate(obj)
        if not 1<=obj.size<=2400000128 or not obj.key.startswith('tenants/'+str(tenant_id)+'/'):
            raise ValueError('RESTORE_ARRAY_CATALOGUE_SCOPE')
        guard()
    guard();return obj


def verify_fixture_produced_array(connection,client,buckets,tenant_id,result,reference,shape,role,*,
                                   assert_fixture_scope,check_active):
    kwargs=dict(assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    before=read_fixture_produced_array(connection,tenant_id,result,reference,role,**kwargs)
    evidence=verify_restore_render_array(client,buckets,reference,before,shape,role,
        assert_fixture_scope=lambda *_:require_restore_fixture(check_active,assert_fixture_scope,
            tenant_id,result,reference,role),check_active=check_active)
    after=read_fixture_produced_array(connection,tenant_id,result,reference,role,**kwargs)
    if before!=after:raise ValueError('RESTORE_ARRAY_CATALOGUE_CHANGED')
    require_restore_fixture(check_active,assert_fixture_scope,tenant_id,result,reference,role)
    return evidence
