"""Execute one positive native render batch through physical worker output."""
from dataclasses import dataclass
from time import monotonic,sleep
from uuid import UUID,uuid5
from hashlib import sha256
import psycopg
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.session.contracts import VersionBinding
from .restore_batch_catalogue import verify_fixture_batch_not_adopted
from .restore_revoke_batch import observe_fixture_render_batch_baseline
from .restore_batch_adoption import verify_fixture_batch_adoption
from .restore_batch_physical import verify_fixture_batch_physical
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreCompletedBatchBaseline:
    schema_version: str
    ticket: object
    adoption: object
    physical: object
    polls: int


def run_fixture_batch_baseline(connection,client,buckets,target,binding,frozen,source_raw,*,expected_database,
                                session_id,tls_context,maximum_seconds,assert_installed_fixture,check_active):
    """No retry of POST; bounded fresh readonly polling, then complete physical proof.

    Operator must run the actual qualified scheduler/worker in fixture isolation.
    This function never starts services, changes fixture SQL or authorizes writers.
    """
    if (type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not isinstance(source_raw,bytes) or not 2<=len(source_raw)<=1048576
            or sha256(source_raw).hexdigest()!=binding.source_sha256
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_BATCH_BASELINE_INSTALLATION')
    deadline=monotonic()+maximum_seconds
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
        if monotonic()>=deadline:raise ValueError('RESTORE_BATCH_BASELINE_TIMEOUT')
    guard()
    verify_fixture_batch_not_adopted(connection,target,binding,expected_database=expected_database,session_id=session_id,
        assert_fixture_scope=lambda *_:guard(),check_active=check_active)
    ticket=observe_fixture_render_batch_baseline(target,binding,frozen,tls_context=tls_context,
        assert_current_fixture=lambda *_:guard(),check_active=check_active)
    adoption=verify_fixture_batch_adoption(connection,target,binding,frozen,ticket,expected_database=expected_database,
        session_id=session_id,assert_fixture_scope=lambda *_:guard(),check_active=check_active)
    polls=0;task=uuid5(adoption.workflow_id,'render.compose:0')
    while True:
        guard()
        if monotonic()>=deadline:raise ValueError('RESTORE_BATCH_BASELINE_TIMEOUT')
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        with connection.transaction():
            connection.execute('SET TRANSACTION READ ONLY');connection.execute("SET LOCAL statement_timeout='5s'")
            connection.execute('SET LOCAL row_security=off')
            scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if scope!=(expected_database,True):raise ValueError('RESTORE_BATCH_BASELINE_AUTHORITY')
            row=connection.execute('SELECT state,cancel_requested FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND id=%s',
                (target.tenant_id,adoption.child_run_id,task)).fetchone()
            polls+=1;guard()
        # A successful row returned after the deadline is still a timed-out
        # observation; statement_timeout does not enforce the overall budget.
        if monotonic()>=deadline:raise ValueError('RESTORE_BATCH_BASELINE_TIMEOUT')
        if not row or len(row)!=2 or row[1] is not False:
            raise ValueError('RESTORE_BATCH_BASELINE_TASK')
        if row[0]=='succeeded':break
        if row[0] not in ('blocked','queued','ready','running','retry_wait'):
            raise ValueError('RESTORE_BATCH_BASELINE_TERMINAL')
        sleep(min(.25,max(0,deadline-monotonic())))
    guard()
    source=VersionBinding(asset_version_id=binding.asset_version_id,
        source=ArtifactRef(artifact_id=binding.source_artifact_id,sha256=binding.source_sha256),canvas_hash=binding.canvas_hash)
    physical=verify_fixture_batch_physical(connection,client,buckets,target.tenant_id,adoption,binding=source,
        source_raw=source_raw,assert_fixture_scope=lambda *_:guard(),check_active=check_active)
    guard()
    return RestoreCompletedBatchBaseline('restore-completed-batch-baseline/v1',ticket,adoption,physical,polls)
