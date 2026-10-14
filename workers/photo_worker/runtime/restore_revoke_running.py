"""Read-only running-render witness for a concurrent consent revoke probe."""
from dataclasses import dataclass
from uuid import UUID,uuid5
import psycopg
from .restore_revoke_probe import RestoreRevokeFixture
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRunningRenderWitness:
    database: str
    tenant_id: UUID
    child_run_id: UUID
    workflow_id: UUID
    task_id: UUID
    attempt: int
    fence: int


@dataclass(frozen=True)
class RestoreCancelledRenderWitness:
    running: RestoreRunningRenderWitness
    state: str
    result_absent: bool


def observe_fixture_cancelled_render(connection,fixture,witness,*,assert_fixture_scope,check_active):
    """Strict cancellation outcome for the observed generation, not a race claim.

    A completed result, changed generation or still-running task refuses. Callers
    may poll using a separate bounded coordinator; this function never retries or
    cancels tasks itself and never authorizes restore writers.
    """
    if (not isinstance(fixture,RestoreRevokeFixture) or not isinstance(witness,RestoreRunningRenderWitness)
            or fixture.tenant_id!=witness.tenant_id
            or witness.workflow_id!=uuid5(witness.child_run_id,'workflow')
            or witness.task_id!=uuid5(witness.workflow_id,'render.compose:0')
            or any(type(v) is not int or v<1 for v in (witness.attempt,witness.fence))
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_CANCELLED_RENDER_INSTALLATION')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,fixture,witness)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(witness.database,True):raise ValueError('RESTORE_CANCELLED_RENDER_AUTHORITY')
        row=connection.execute('''SELECT EXISTS(SELECT FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.workflow_consent_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id
 JOIN photo.consent_grants g ON g.tenant_id=l.tenant_id AND g.project_id=l.project_id AND g.id=l.consent_id
 JOIN photo.task_attempts a ON a.tenant_id=t.tenant_id AND a.task_id=t.id AND a.attempt=t.attempt
 WHERE t.tenant_id=%s AND r.id=%s AND w.id=%s AND w.project_id=%s AND t.id=%s
 AND t.operation='render.compose' AND t.operation_version='1'
 AND t.attempt=%s AND t.fencing_token=%s AND a.fencing_token=t.fencing_token
 AND a.finished_at IS NOT NULL AND t.state='cancelled' AND t.cancel_requested AND r.cancel_requested
 AND t.lease_until IS NULL AND t.canonical_result_id IS NULL
 AND l.project_id=w.project_id AND l.consent_id=%s AND l.consent_epoch=%s
 AND g.status='revoked' AND g.epoch=l.consent_epoch+1
 AND g.purpose=%s AND g.person_id IS NOT DISTINCT FROM %s
 AND NOT EXISTS(SELECT FROM photo.task_results result WHERE result.tenant_id=t.tenant_id AND result.task_id=t.id))''',
            (fixture.tenant_id,witness.child_run_id,witness.workflow_id,fixture.project_id,witness.task_id,
             witness.attempt,witness.fence,fixture.consent_id,fixture.epoch,fixture.purpose,fixture.person_id)).fetchone()
        if row!=(True,):raise ValueError('RESTORE_CANCELLED_RENDER_NOT_SETTLED')
        guard()
    guard()
    return RestoreCancelledRenderWitness(witness,'cancelled',True)


def observe_fixture_running_render(connection,fixture,child_run_id,*,expected_database,
                                   assert_fixture_scope,check_active):
    """Require a real live leased task, before result/adoption, linked to grant.

    This is an observation, not a synchronization barrier: a complete race probe
    must revalidate this witness around revoke and prove adoption exclusion.
    """
    if (not isinstance(fixture,RestoreRevokeFixture) or not isinstance(child_run_id,UUID)
            or child_run_id.int==0 or not isinstance(expected_database,str) or not expected_database
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_RUNNING_RENDER_INSTALLATION')
    workflow=uuid5(child_run_id,'workflow');task=uuid5(workflow,'render.compose:0')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,fixture,child_run_id)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_RUNNING_RENDER_AUTHORITY')
        row=connection.execute('''SELECT t.attempt,t.fencing_token
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.workflow_consent_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id
 JOIN photo.consent_grants g ON g.tenant_id=l.tenant_id AND g.project_id=l.project_id AND g.id=l.consent_id
 JOIN photo.task_attempts a ON a.tenant_id=t.tenant_id AND a.task_id=t.id AND a.attempt=t.attempt
 WHERE t.tenant_id=%s AND r.id=%s AND w.id=%s AND w.project_id=%s
 AND t.id=%s AND t.operation='render.compose' AND t.operation_version='1'
 AND r.graph_sealed AND r.status='running' AND NOT r.cancel_requested
 AND t.state='running' AND NOT t.cancel_requested AND t.lease_until>clock_timestamp()
 AND a.fencing_token=t.fencing_token AND a.finished_at IS NULL AND a.error IS NULL
 AND l.project_id=w.project_id AND l.consent_id=%s AND l.consent_epoch=%s
 AND g.status='granted' AND g.epoch=l.consent_epoch AND g.purpose=%s AND g.person_id IS NOT DISTINCT FROM %s
 AND NOT EXISTS(SELECT FROM photo.task_results result WHERE result.tenant_id=t.tenant_id AND result.task_id=t.id)
 AND NOT EXISTS(SELECT FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id IN (SELECT photo.v2_adoption_event_id(t.id,result.id) FROM photo.task_results result
 WHERE result.tenant_id=t.tenant_id AND result.task_id=t.id))''',
            (fixture.tenant_id,child_run_id,workflow,fixture.project_id,task,fixture.consent_id,fixture.epoch,
             fixture.purpose,fixture.person_id)).fetchone()
        if (not row or len(row)!=2 or any(type(v) is not int or v<1 for v in row)):
            raise ValueError('RESTORE_RUNNING_RENDER_NOT_LIVE')
        guard()
    guard()
    return RestoreRunningRenderWitness(expected_database,fixture.tenant_id,child_run_id,workflow,task,*row)
