"""Independent canonical successful render task/attempt/fence catalogue."""
from dataclasses import dataclass
from uuid import UUID,uuid5
import psycopg
from .restore_inventory import RestoreObject,_validate
from .restore_batch_adoption import RestoreBatchAdoptionEvidence
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreBatchResultEvidence:
    schema_version: str
    database: str
    task_id: UUID
    result_id: UUID
    artifact_id: UUID
    attempt: int
    fence: int
    object: RestoreObject


def observe_fixture_batch_result(connection,tenant_id,adoption,*,assert_fixture_scope,check_active):
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0 or not isinstance(adoption,RestoreBatchAdoptionEvidence)
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_BATCH_RESULT_INSTALLATION')
    task=uuid5(adoption.workflow_id,'render.compose:0')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,tenant_id,adoption)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if scope!=(adoption.database,True):raise ValueError('RESTORE_BATCH_RESULT_AUTHORITY')
        row=connection.execute('''SELECT z.id,a.id,t.attempt,t.fencing_token,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id
 AND attempt.attempt=z.attempt AND attempt.fencing_token=z.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.id::text=z.outputs->0->>'artifact_id'
 WHERE t.tenant_id=%s AND t.run_id=%s AND t.id=%s AND t.node_key='render.compose:0'
 AND t.state='succeeded' AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND attempt.finished_at IS NOT NULL AND attempt.error IS NULL
 AND a.sha256=z.outputs->0->>'sha256' AND photo.v26_render_task_result_ready(t.tenant_id,t.id)''',
            (tenant_id,adoption.child_run_id,task)).fetchone()
        if not row or len(row)!=9:raise ValueError('RESTORE_BATCH_RESULT_NOT_READY')
        result_id,artifact_id,attempt,fence,*physical=row
        if (any(not isinstance(v,UUID) or v.int==0 for v in (result_id,artifact_id))
                or any(type(n) is not int or n<1 for n in (attempt,fence))):
            raise ValueError('RESTORE_BATCH_RESULT_IDENTITY')
        ref=RestoreObject(*physical);_validate(ref)
        if not 2<=ref.size<=1048576 or not ref.key.startswith('tenants/'+str(tenant_id)+'/'):
            raise ValueError('RESTORE_BATCH_RESULT_OBJECT')
        guard()
    guard()
    return RestoreBatchResultEvidence('restore-batch-result/v1',adoption.database,task,result_id,artifact_id,attempt,fence,ref)
