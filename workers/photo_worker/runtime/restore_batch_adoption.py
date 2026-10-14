"""Read-only proof of one-frame batch ticket registration and sealed child graph."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID,uuid5
import psycopg
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_revoke_batch import RestoreRenderBatchTarget,RestoreRenderBatchBaseline,render_batch_request_body
from .restore_revoke_freeze import RestoreFreezeBaselineBinding,RestoreFreezeBaselineEvidence
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreBatchAdoptionEvidence:
    schema_version: str
    database: str
    batch_id: UUID
    child_run_id: UUID
    workflow_id: UUID


def verify_fixture_batch_adoption(connection,target,binding,frozen,ticket,*,expected_database,session_id,
                                   assert_fixture_scope,check_active):
    if (not isinstance(target,RestoreRenderBatchTarget) or not isinstance(binding,RestoreFreezeBaselineBinding)
            or not isinstance(frozen,RestoreFreezeBaselineEvidence) or not isinstance(ticket,RestoreRenderBatchBaseline)
            or not isinstance(session_id,UUID) or session_id.int==0
            or not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_BATCH_ADOPTION_INSTALLATION')
    batch=uuid5(target.tenant_id,'V2.Admit:'+str(binding.actor_id)+':'+target.idempotency_key)
    child=uuid5(batch,'render.batch:0');workflow=uuid5(child,'workflow')
    request=render_batch_request_body(target,binding,frozen)
    if ticket.batch_id!=batch or ticket.child_run_id!=child or ticket.request_sha256!=sha256(request).hexdigest():
        raise ValueError('RESTORE_BATCH_ADOPTION_TICKET')
    request_sha=sha256(canonical_bytes(dict(project_id=str(target.project_id),request=parse_json(request)))).hexdigest()
    response=dict(run_id=str(batch),state=ticket.state,child_run_ids=[str(child)])
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,frozen,ticket)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if scope!=(expected_database,True):raise ValueError('RESTORE_BATCH_ADOPTION_AUTHORITY')
        row=connection.execute('''SELECT EXISTS(SELECT FROM photo.idempotency_records i
 JOIN photo.runs parent ON parent.tenant_id=i.tenant_id AND parent.id=i.resource_id
 JOIN photo.runs child ON child.tenant_id=parent.tenant_id AND child.id=%s
 JOIN photo.v2_workflows w ON w.tenant_id=child.tenant_id AND w.run_id=child.id
 WHERE i.tenant_id=%s AND i.principal_type='user' AND i.principal_id=%s AND i.route='V2.Admit' AND i.key=%s
 AND i.request_hash=%s AND i.resource_id=%s AND i.response_status=202 AND i.response_body=%s::jsonb
 AND parent.created_by=%s AND parent.input_snapshot_id=%s
 AND jsonb_array_length(parent.config_snapshot->'parts')=1
 AND parent.config_snapshot->'parts'->0->>'run_id'=%s
 AND parent.config_snapshot->>'project_id'=%s AND child.kind='v2' AND child.graph_sealed
 AND child.created_by=parent.created_by AND child.input_snapshot_id=parent.input_snapshot_id
 AND child.config_hash=parent.config_snapshot->'parts'->0->>'config_sha'
 AND child.config_snapshot->'v26_render'->>'auth_session_id'=%s
 AND w.id=%s AND w.project_id=%s AND w.input_snapshot_id=%s
 AND EXISTS(SELECT FROM photo.tasks task WHERE task.tenant_id=child.tenant_id AND task.run_id=child.id
 AND task.operation='render.compose' AND task.operation_version='1'))''',
            (child,target.tenant_id,binding.actor_id,target.idempotency_key,request_sha,batch,
             canonical_bytes(response).decode(),binding.actor_id,target.snapshot_id,str(child),str(target.project_id),
             str(session_id),workflow,target.project_id,target.snapshot_id)).fetchone()
        if row!=(True,):raise ValueError('RESTORE_BATCH_ADOPTION_GRAPH')
        guard()
    guard()
    return RestoreBatchAdoptionEvidence('restore-batch-adoption/v1',expected_database,batch,child,workflow)
