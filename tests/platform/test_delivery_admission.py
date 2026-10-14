"""Admission wiring tests; SQL role/locking evidence requires real DB later."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import UUID
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.delivery import V2DeliveryPrepareParams
from photo_worker.operations.delivery.admission import SQLDeliveryAdmission
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure


def fixture():
    params = V2DeliveryPrepareParams.model_validate_json('{"schema_version":1,"input":{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"'+'a'*64+'"}}')
    command = SimpleNamespace(operation='v2.delivery.prepare', operation_version='1', resource_class='cpu',
        tenant_id=UUID(int=2), run_id=UUID(int=3), task_id=UUID(int=4), attempt=7,
        params=SimpleNamespace(artifact_id=UUID(int=5), sha256=sha256(canonical_bytes(params)).hexdigest()))
    lease = SimpleNamespace(command=command, worker_id=UUID(int=6), fence=9,
        execution_profile={'renderer_version': 'v2.2', 'backend': 'cpu'}, input_digest='b'*64)
    ctx = SimpleNamespace(lease=lease, check_active=lambda: None)
    return ctx, params


def test_real_database_adapter_binds_exact_tenant_attempt_fence_and_params():
    ctx, params = fixture()
    database = object.__new__(TaskDatabase)
    calls = []
    database._query = lambda sql, args: calls.append((sql, args)) or (True,)
    source = []
    def verify(ctx, params): source.append(params);return True
    SQLDeliveryAdmission(database, verify).admit(ctx, params)
    command = ctx.lease.command
    assert calls == [('SELECT photo.v30_delivery_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (command.tenant_id, command.run_id, command.task_id, 7, 9, ctx.lease.worker_id,
         command.params.artifact_id, command.params.sha256, canonical_bytes(params).decode('utf-8'), 'b'*64))]
    assert source == [params]


@pytest.mark.parametrize('row', [None, (False,), (None,), (1,)])
def test_nontrue_lease_gate_never_calls_source_verifier(row):
    ctx, params = fixture()
    database = object.__new__(TaskDatabase);database._query = lambda sql, args: row
    calls = []
    with pytest.raises(OperationFailure):
        SQLDeliveryAdmission(database, lambda ctx, params: calls.append(params) or True).admit(ctx, params)
    assert calls == []


def test_valid_lease_cannot_replace_source_authority():
    ctx, params = fixture()
    database = SimpleNamespace(delivery_lease_ready=lambda lease, raw: True)
    for result in (None, False, 1):
        with pytest.raises(OperationFailure): SQLDeliveryAdmission(database, lambda ctx, params: result).admit(ctx, params)
    with pytest.raises(ValueError): SQLDeliveryAdmission(database, None)


def test_changed_params_are_refused_before_sql_or_source():
    ctx, params = fixture();ctx.lease.command.params.sha256 = 'f'*64
    calls = []
    database = SimpleNamespace(delivery_lease_ready=lambda *args: calls.append(args) or True)
    with pytest.raises(OperationFailure): SQLDeliveryAdmission(database, lambda ctx, params: True).admit(ctx, params)
    assert calls == []

def test_default_source_gate_binds_same_lease_and_cannot_accept_false():
    ctx, params = fixture()
    database = object.__new__(TaskDatabase)
    calls = []
    def query(sql, args):
        calls.append((sql, args))
        return (False,) if 'v30_delivery_source_ready' in sql else (True,)
    database._query = query
    with pytest.raises(OperationFailure): SQLDeliveryAdmission(database).admit(ctx, params)
    assert len(calls) == 2
    assert 'v30_delivery_lease_ready' in calls[0][0]
    assert 'v30_delivery_source_ready' in calls[1][0]
    assert calls[0][1] == calls[1][1]


def test_default_source_gate_requires_explicit_true():
    ctx, params = fixture()
    database = object.__new__(TaskDatabase)
    database._query = lambda sql, args: (True,)
    SQLDeliveryAdmission(database).admit(ctx, params)


@pytest.mark.parametrize('profile', [None, [], 'cpu', True, 1])
def test_direct_admission_rejects_malformed_profile_before_sql(profile):
    ctx, params = fixture()
    ctx.lease.execution_profile = profile
    calls = []
    database = SimpleNamespace(delivery_lease_ready=lambda *args: calls.append(args) or True)
    with pytest.raises(OperationFailure, match='DELIVERY_SCOPE_STALE'):
        SQLDeliveryAdmission(database, lambda ctx, value: True).admit(ctx, params)
    assert calls == []


@pytest.mark.parametrize('boundary', ['lease', 'source_original', 'source_copy', 'final_active'])
def test_admission_refuses_parameter_mutation_between_authority_checks(boundary):
    ctx, params = fixture()
    original_raw = canonical_bytes(params)
    seen = []
    checks = []

    def active():
        checks.append(True)
        if boundary == 'final_active' and len(checks) == 3:
            params.input.sha256 = 'f' * 64

    ctx.check_active = active

    def lease_ready(lease, raw):
        assert raw == original_raw
        if boundary == 'lease':
            params.input.sha256 = 'f' * 64
        return True

    def source(context, value):
        assert value is not params
        assert value.input is not params.input
        assert canonical_bytes(value) == original_raw
        seen.append(True)
        if boundary == 'source_original':
            params.input.sha256 = 'f' * 64
        if boundary == 'source_copy':
            value.input.sha256 = 'f' * 64
        return True

    database = SimpleNamespace(delivery_lease_ready=lease_ready)
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        SQLDeliveryAdmission(database, source).admit(ctx, params)
    assert len(seen) == (0 if boundary == 'lease' else 1)
    if boundary == 'source_copy':
        assert canonical_bytes(params) == original_raw
