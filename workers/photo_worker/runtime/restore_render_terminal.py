"""Read-only proof that the reviewed V2 child and its one-frame parent settled."""
from dataclasses import dataclass
from uuid import UUID,uuid5
import psycopg
from .restore_batch_adoption import RestoreBatchAdoptionEvidence
from .restore_render_review import RestoreRenderReviewEvidence
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderTerminalEvidence:
    schema_version: str
    database: str
    batch_id: UUID
    child_run_id: UUID
    workflow_id: UUID
    review_id: UUID
    parent_revision: int
    child_revision: int


def observe_fixture_render_terminal(connection,tenant_id,actor_id,adoption,review,*,
                                    assert_fixture_scope,check_active):
    """Require complete parent/child state, exact review and no pending sibling."""
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(actor_id,UUID) or actor_id.int==0
            or not isinstance(adoption,RestoreBatchAdoptionEvidence)
            or not isinstance(review,RestoreRenderReviewEvidence)
            or review.database!=adoption.database
            or review.task_id!=uuid5(adoption.workflow_id,'render.compose:0')
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_TERMINAL_INSTALLATION')
    def guard():require_restore_fixture(check_active,assert_fixture_scope)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute("SET LOCAL lock_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(adoption.database,True):raise ValueError('RESTORE_RENDER_TERMINAL_AUTHORITY')
        rows=connection.execute('''SELECT parent.revision,child.revision
 FROM photo.runs parent
 JOIN photo.runs child ON child.tenant_id=parent.tenant_id AND child.id=%s
 JOIN photo.v2_workflows w ON w.tenant_id=child.tenant_id AND w.run_id=child.id
 JOIN photo.tasks t ON t.tenant_id=child.tenant_id AND t.run_id=child.id AND t.id=%s
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.reviews v ON v.tenant_id=child.tenant_id AND v.run_id=child.id AND v.task_id=t.id
 WHERE parent.tenant_id=%s AND parent.id=%s AND parent.kind='bulk'
  AND parent.status='completed' AND parent.graph_sealed AND NOT parent.cancel_requested
  AND parent.created_by=%s AND parent.config_snapshot->'schema_version'='1'::jsonb
  AND jsonb_typeof(parent.config_snapshot->'parts')='array'
  AND jsonb_array_length(parent.config_snapshot->'parts')=1
  AND parent.config_snapshot->'parts'->0->>'run_id'=child.id::text
  AND parent.config_snapshot->'parts'->0->>'config_sha'=child.config_hash
  AND parent.config_snapshot->'parts'->0->>'frame_count'='1'
  AND child.kind='v2' AND child.status='completed' AND child.graph_sealed
  AND NOT child.cancel_requested AND child.created_by=parent.created_by
  AND child.input_snapshot_id=parent.input_snapshot_id
  AND child.config_snapshot->>'project_id'=parent.config_snapshot->>'project_id'
  AND child.config_snapshot->'v26_render'->>'auth_session_id'=parent.config_snapshot->>'auth_session_id'
  AND jsonb_typeof(child.config_snapshot->'v26_render'->'nodes')='array'
  AND jsonb_array_length(child.config_snapshot->'v26_render'->'nodes')=1
  AND child.config_snapshot->'v26_render'->'nodes'->0->>'task_id'=t.id::text
  AND w.id=%s AND w.workflow_type='preview'
  AND t.operation='render.compose' AND t.operation_version='1' AND t.state='succeeded'
  AND NOT t.cancel_requested AND z.review_required
  AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  AND photo.v26_render_task_result_ready(t.tenant_id,t.id)
  AND v.id=%s AND v.state='decided' AND v.kind='render.compose'
  AND v.decision='accept' AND v.decision_route='candidate' AND v.decided_by=%s
  AND v.decision_hash=%s AND v.revision=%s
  AND v.candidate_artifact_id=%s AND v.candidate_hash=%s
  AND NOT EXISTS(SELECT FROM photo.reviews other WHERE other.tenant_id=child.tenant_id
   AND other.run_id=child.id AND (other.id<>v.id OR other.state<>'decided'))
  AND NOT EXISTS(SELECT FROM photo.tasks other WHERE other.tenant_id=child.tenant_id
   AND other.run_id=child.id AND other.id<>t.id)
 LIMIT 2''',(
            adoption.child_run_id,review.task_id,tenant_id,adoption.batch_id,
            actor_id,adoption.workflow_id,review.review_id,actor_id,
            review.decision_sha256,review.revision,
            review.candidate.artifact_id,review.candidate.sha256)).fetchall()
        guard()
    if len(rows)!=1 or len(rows[0])!=2:
        raise ValueError('RESTORE_RENDER_TERMINAL_NOT_COMPLETE')
    parent_revision,child_revision=rows[0]
    if (any(type(v) is not int or not 1<=v<9007199254740991
            for v in (parent_revision,child_revision))):
        raise ValueError('RESTORE_RENDER_TERMINAL_REVISION')
    guard()
    return RestoreRenderTerminalEvidence('restore-render-terminal/v1',adoption.database,
        adoption.batch_id,adoption.child_run_id,adoption.workflow_id,review.review_id,
        parent_revision,child_revision)
