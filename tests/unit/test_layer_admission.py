from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.operations.layers.admission import LensUserLayerAdmission
from photo_worker.runtime.database import Lease, TaskDatabase
from photo_worker.runtime.executor import ExecutionContext, LeaseLost, OperationFailure


def fixture(tmp_path, accepted=True):
    from test_layer_parents import setup
    params, *_ = setup(tmp_path)
    params = params.model_copy(update={'source_result': None})
    command = SimpleNamespace(operation='layer.compose', operation_version='1', resource_class='cpu',
        tenant_id=uuid4(), run_id=uuid4(), task_id=uuid4(), attempt=2,
        params=ArtifactRef(artifact_id=uuid4(), sha256=canonical_hash(params)))
    now = datetime.now(timezone.utc)
    lease = Lease(command, uuid4(), 7, now+timedelta(minutes=1), now+timedelta(minutes=2),
                  {'renderer_version': 'v2.2', 'backend': 'cpu'}, 'e'*64)
    calls = []
    class Database:
        def admit_lens_user_composition(self, received, raw):
            calls.append((received, raw))
            return accepted
    return params, ExecutionContext(lease), LensUserLayerAdmission(Database()), calls


def test_admission_forwards_exact_lease_and_canonical_parameters(tmp_path):
    params, ctx, admission, calls = fixture(tmp_path)
    admission.admit(ctx, params)
    assert calls == [(ctx.lease, canonical_bytes(params))]


def test_database_denial_is_fail_closed(tmp_path):
    params, ctx, admission, calls = fixture(tmp_path, False)
    with pytest.raises(OperationFailure) as caught:
        admission.admit(ctx, params)
    assert caught.value.error.code == 'LAYER_SCOPE_STALE' and len(calls) == 1


@pytest.mark.parametrize('change', ['hash', 'operation', 'backend', 'parent'])
def test_unbound_parameters_and_unsupported_paths_never_query_sql(tmp_path, change):
    params, ctx, admission, calls = fixture(tmp_path)
    if change == 'hash':
        ctx.lease.command.params = ArtifactRef(artifact_id=uuid4(), sha256='0'*64)
    elif change == 'operation':
        ctx.lease.command.operation = 'optical.lens'
    elif change == 'backend':
        ctx.lease.execution_profile['backend'] = 'cuda'
    else:
        params = params.model_copy(update={'source_result': params.proposal_result})
    with pytest.raises(OperationFailure):
        admission.admit(ctx, params)
    assert calls == []


def test_lost_lease_stops_admission_before_database(tmp_path):
    params, ctx, admission, calls = fixture(tmp_path)
    ctx._lost.set()
    with pytest.raises(LeaseLost):
        admission.admit(ctx, params)
    assert calls == []


def test_sql_binding_contains_all_tenant_and_attempt_fences(tmp_path):
    params, ctx, _, _ = fixture(tmp_path)
    database = TaskDatabase('unused')
    calls = []
    database._query = lambda query, args: calls.append((query, args)) or (True,)
    raw = canonical_bytes(params)
    assert database.admit_lens_user_composition(ctx.lease, raw)
    command = ctx.lease.command
    assert calls[0][1] == (command.tenant_id, command.run_id, command.task_id, 2, 7,
        ctx.lease.worker_id, command.params.artifact_id, command.params.sha256, raw.decode(), 'e'*64)
    database._query = lambda *args: None
    assert not database.admit_lens_user_composition(ctx.lease, raw)
