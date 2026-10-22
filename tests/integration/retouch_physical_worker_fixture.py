"""Real SQL dispatch/claim and CPU staging for versioned retouch artifacts.

The connection adapter only changes transport: TaskDatabase retains its production
claim decoding and worker-only SQL functions. Upstream qualification/face fixtures
are synthetic and do not constitute photographic qualification.
"""
from photo_worker.contracts.wire import TaskCommand
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from retouch_preview_admission_fixture import prepared_retouch_preview_fixture
from test_v27_retouch_preview_worker_claim import dispatched_preview_fixture


class ConnectionWorkerDatabase(TaskDatabase):
    """Use the disposable fixture transaction; never open a production DSN."""
    def __init__(self, connection):
        self.connection = connection

    def _query(self, statement, parameters):
        with self.connection.transaction():
            self.connection.execute('SET LOCAL ROLE photo_worker')
            try:
                # Roll back a refused SQL call before restoring the role; otherwise
                # RESET would mask the original error in an aborted transaction.
                with self.connection.transaction():
                    return self.connection.execute(statement, parameters).fetchone()
            finally:
                self.connection.execute('RESET ROLE')


def claimed_physical_preview_fixture(db, objects, *, with_alpha=False):
    prepared = prepared_retouch_preview_fixture(db, objects=objects, with_alpha=with_alpha)
    tenant, task, run, worker, command, old_fence = dispatched_preview_fixture(db, prepared=prepared)
    database = ConnectionWorkerDatabase(db)
    typed_command = TaskCommand.model_validate_json(canonical_bytes(command))
    claim = database.claim(typed_command, worker)
    assert claim.outcome == 'claimed' and claim.lease is not None
    assert claim.lease.fence == old_fence + 1
    assert claim.lease.command.tenant_id == tenant
    assert claim.lease.command.task_id == task and claim.lease.command.run_id == run
    context = ExecutionContext(claim.lease)
    context.check_active()
    return prepared, database, context


def execute_physical_preview_fixture(db, objects, root, *, with_alpha=False):
    """Compute and stage the real eight-role preview; completion stays separate.

    Uses real local ICC profiles, full CPU compute/preservation/media stages,
    live SQL admission/origin/report gates and physical output publication.
    Imported fixture qualification is not a real-photo quality certification.
    """
    prepared, database, context = claimed_physical_preview_fixture(db, objects, with_alpha=with_alpha)
    result = stage_physical_preview(db, objects, root, database, context)
    return prepared, database, context, result


def stage_physical_preview(db, objects, root, database, context):
    """Common real compute path for staged and Executor-completed fixtures."""
    from hashlib import sha256
    from pathlib import Path
    from photo_worker.color.icc import ICCRegistry
    from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
    from photo_worker.operations.semantic.preview_retouch_adapter import execute_retouch_preview_task
    from photo_worker.operations.semantic.preview_retouch_installation import sql_retouch_preview_factory
    from retouch_physical_gateway_fixture import physical_task_artifacts

    icc_root = Path(__file__).resolve().parents[2] / 'resources' / 'icc'
    registry = ICCRegistry(icc_root)
    presentation = PreviewPresentationPins(icc_root,
        sha256((icc_root / 'profiles.lock.json').read_bytes()).hexdigest(),
        registry.resolve('srgb_v1').sha256)
    profile = context.lease.execution_profile
    installer = sql_retouch_preview_factory(database=database, objects=objects[0], bucket=objects[1],
        icc_registry=registry, model_lock_sha=profile['model_lock_sha'],
        runtime_lock_sha=profile['runtime_lock_sha'], device_profile_sha=profile['device_profile_sha'],
        dataset_sha=profile['dataset_sha'], max_ram_bytes=profile['resource_estimate']['ram_bytes'])
    with physical_task_artifacts(db, objects, context, root) as artifacts:
        installation = installer(context, artifacts)
        result = execute_retouch_preview_task(context, artifacts, installation, presentation)
    context.check_active()
    return result


def completed_physical_preview_fixture(db, objects, root, *, with_alpha=False):
    """Run production Executor, including claim, heartbeat, metrics and commit.

    Commits preparation in a freshly isolated photo_test database because actual
    TaskDatabase calls use separate connections. Never accepts a deployed DB.
    The gateway transport connection is autocommit only during execution so
    uploaded outputs are visible to the worker's final SQL commit.
    """
    import re
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.runtime.executor import Executor, Operation
    if re.fullmatch(r'photo_test_[0-9a-f]{32}', db.info.dbname) is None:
        raise ValueError('Fresh disposable photo_test database required')
    prepared = prepared_retouch_preview_fixture(db, objects=objects, with_alpha=with_alpha)
    _, _, _, worker, command, _ = dispatched_preview_fixture(db, prepared=prepared)
    db.commit()
    previous_autocommit = db.autocommit
    captured = {}
    from wire_runtime_login import worker_login
    try:
        db.autocommit = True
        # Preparation is committed; bind the queued worker registration to this
        # exact nonprivileged LOGIN before Executor's actual pending claim.
        with worker_login(db.info.dsn, prepared['task'], pending_dispatch=True) as runtime_dsn:
            worker_database = TaskDatabase(runtime_dsn)
            def execute(context):
                captured['context'] = context
                try:
                    result = stage_physical_preview(db, objects, root, worker_database, context)
                except Exception as error:
                    # Preserve the actual cause for test diagnostics. Do not print arbitrary
                    # error text, which may contain transport URLs or credentials.
                    from pathlib import Path
                    from retouch_execution_diagnostic import safe_execution_error
                    captured['execution_diagnostic'] = safe_execution_error(error,Path(__file__).resolve().parents[2])
                    raise
                captured['result'] = result
                return result
            operation = Operation('semantic.preview_retouch_region', 'cpu', execute, '1')
            executor = Executor(worker_database, worker, [operation], heartbeat_seconds=1)
            outcome = executor.process(canonical_bytes(command))
            if outcome != 'committed' or 'result' not in captured:
                raise AssertionError(dict(message='Physical retouch Executor did not commit success',
                    outcome=outcome, execution_error_type=captured.get('execution_diagnostic',{}).get('type'),
                    execution_diagnostic=captured.get('execution_diagnostic')))
            assert executor.process(canonical_bytes(command)) == 'terminal'
    finally:
        db.autocommit = previous_autocommit
    # The scoped runtime handle expires at return; current consumers discard it.
    # Return shape retained for existing metadata/result consumers, never reuse
    # this handle for further runtime I/O after its LOGIN has been removed.
    return prepared, worker_database, captured['context'], captured['result']
