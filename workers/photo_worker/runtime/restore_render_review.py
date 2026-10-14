"""Read-only proof that a reviewed render selected its exact adopted image."""
from dataclasses import dataclass
from datetime import datetime
import re
from uuid import UUID, uuid5
import psycopg
from photo_worker.contracts.base import ArtifactRef
from .restore_batch_adoption import RestoreBatchAdoptionEvidence
from .restore_batch_physical import RestoreBatchPhysicalEvidence
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderReviewEvidence:
    schema_version: str
    database: str
    review_id: UUID
    task_id: UUID
    candidate: ArtifactRef
    revision: int
    decision_sha256: str
    decided_at: datetime


def observe_fixture_render_review(connection,tenant_id,actor_id,adoption,physical,*,
                                  assert_fixture_scope,check_active):
    """Require one accepted decision on the exact adopted render child.

    This observes a decision; it neither creates review nor proves an exported
    download or a completed parent workflow.
    """
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(actor_id,UUID) or actor_id.int==0
            or not isinstance(adoption,RestoreBatchAdoptionEvidence)
            or not isinstance(physical,RestoreBatchPhysicalEvidence)
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_REVIEW_INSTALLATION')
    task=uuid5(adoption.workflow_id,'render.compose:0')
    candidate=physical.image_result.working_image
    result=physical.result
    if (not isinstance(candidate,ArtifactRef) or candidate.artifact_id.int==0
            or not isinstance(result.task_id,UUID) or result.task_id!=task
            or result.database!=adoption.database
            or physical.schema_version!='restore-batch-physical/v1'):
        raise ValueError('RESTORE_RENDER_REVIEW_PHYSICAL')
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
        if authority!=(adoption.database,True):
            raise ValueError('RESTORE_RENDER_REVIEW_AUTHORITY')
        rows=connection.execute('''SELECT v.id,v.revision,v.decision_hash,v.decided_at
 FROM photo.reviews v
 JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
 JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=w.tenant_id
  AND root_link.workflow_id=w.id AND root_link.project_id=w.project_id
 JOIN photo.v2_artifact_links child_link ON child_link.tenant_id=w.tenant_id
  AND child_link.workflow_id=w.id AND child_link.project_id=w.project_id
 JOIN photo.artifacts c ON c.tenant_id=v.tenant_id AND c.id=v.candidate_artifact_id
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.id=%s
 WHERE v.tenant_id=%s AND v.run_id=%s AND v.task_id=%s
  AND v.kind='render.compose' AND v.state='decided' AND v.decision='accept'
  AND v.decision_route='candidate' AND v.decided_by=%s
  AND v.candidate_artifact_id=%s AND v.candidate_hash=%s
  AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.status IN ('waiting_review','completed')
  AND w.id=%s AND w.workflow_type='preview'
  AND t.operation='render.compose' AND t.operation_version='1'
  AND t.state='succeeded' AND NOT t.cancel_requested
  AND z.id=%s AND z.attempt=%s AND z.fencing_token=%s AND z.review_required
  AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
  AND root.id=root_link.artifact_id AND root.sha256=root_link.sha256
  AND root_link.ordinal=0
  AND root_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
  AND root.sha256=%s AND root.state='ready' AND root.verified_at IS NOT NULL
  AND root.created_by_task_id=t.id AND root.produced_attempt=%s AND root.produced_fence=%s
  AND c.id=child_link.artifact_id AND c.sha256=child_link.sha256
  AND child_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
  AND c.sha256=%s AND c.state='ready' AND c.verified_at IS NOT NULL
  AND c.created_by_task_id=t.id AND c.produced_attempt=%s AND c.produced_fence=%s
  AND coalesce(root.object_version,'') NOT IN ('','null')
  AND coalesce(c.object_version,'') NOT IN ('','null')
  AND (root.expires_at IS NULL OR root.expires_at>transaction_timestamp())
  AND (c.expires_at IS NULL OR c.expires_at>transaction_timestamp())
  AND EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id
   AND i.task_id=t.id AND i.artifact_id=v.base_artifact_id AND i.contract='WorkingImage')
 LIMIT 2''',(
            result.artifact_id,tenant_id,adoption.child_run_id,task,actor_id,
            candidate.artifact_id,candidate.sha256,adoption.workflow_id,
            result.result_id,result.attempt,result.fence,result.object.sha256,
            result.attempt,result.fence,candidate.sha256,result.attempt,result.fence)).fetchall()
        guard()
    if len(rows)!=1 or len(rows[0])!=4:
        raise ValueError('RESTORE_RENDER_REVIEW_NOT_COMPLETE')
    review_id,revision,decision_sha,decided_at=rows[0]
    if (not isinstance(review_id,UUID) or review_id.int==0
            or type(revision) is not int or revision<2
            or not isinstance(decision_sha,str)
            or re.fullmatch('[0-9a-f]{64}',decision_sha) is None
            or not isinstance(decided_at,datetime) or decided_at.tzinfo is None):
        raise ValueError('RESTORE_RENDER_REVIEW_EVIDENCE')
    guard()
    return RestoreRenderReviewEvidence('restore-render-review/v1',adoption.database,
                                       review_id,task,candidate,revision,decision_sha,decided_at)
