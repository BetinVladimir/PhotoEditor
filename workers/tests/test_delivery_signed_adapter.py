from types import SimpleNamespace
from unittest.mock import MagicMock, Mock, patch
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.delivery import (
    SignedDeliveryAssemblyEnvelope, SignedDeliveryManifest, V2DeliveryAssembleParams,
)
from photo_worker.operations.delivery.signed_adapter import (
    SQLSignedDeliveryAdmission, SignedDeliveryInstallation, execute_signed_assembly,
)
from photo_worker.operations.delivery.adapter import DeliveryInstallation, delivery_operations
from photo_worker.runtime.executor import OperationFailure


def _fixture():
    tenant, actor, parent = uuid4(), uuid4(), uuid4()
    envelope_ref = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    assembly_ref = ArtifactRef(artifact_id=uuid4(), sha256='b' * 64)
    closure_ref = ArtifactRef(artifact_id=uuid4(), sha256='c' * 64)
    envelope = SignedDeliveryAssemblyEnvelope(schema_version=2,
        assembly=assembly_ref, signing_closure=closure_ref)
    params = V2DeliveryAssembleParams(schema_version=1, assembly=envelope_ref)
    result = SignedDeliveryManifest(schema_version=2, envelope_sha=envelope_ref.sha256,
        assembly=assembly_ref, signing_closure=closure_ref, snapshot_digest='d' * 64,
        slot_count=1, ready_count=1, omitted_count=0, shards=[assembly_ref])
    ctx = SimpleNamespace(check_active=lambda: None, lease=SimpleNamespace(
        execution_profile={'renderer_version':'v2.2','backend':'cpu'},
        command=SimpleNamespace(resource_class='cpu', operation_version='1',
                                operation='v2.delivery.assemble')))
    return (tenant, actor, parent), envelope_ref, envelope, params, result, ctx


def test_signed_adapter_checks_same_scope_before_and_after_compute():
    scope, envelope_ref, envelope, params, result, ctx = _fixture()
    events = []
    class Admission:
        def admit_signed(self, _ctx, candidate):
            assert candidate == params
            events.append('admit')
            return scope
    artifacts = Mock()
    artifacts.params.return_value = params
    artifacts.contract_bytes.return_value = canonical_bytes(envelope)
    artifacts.stage_contract.return_value = envelope_ref
    with patch('photo_worker.operations.delivery.signed_adapter.assemble_signed_delivery',
               side_effect=lambda *args: (events.append('assemble'), result)[1]):
        completed = execute_signed_assembly(ctx, artifacts,
            SignedDeliveryInstallation(admission=Admission()))
    assert events == ['admit', 'assemble', 'admit']
    assert completed.outputs == [envelope_ref]
    artifacts.contract_bytes.assert_called_once_with(envelope_ref, SignedDeliveryAssemblyEnvelope)
    artifacts.stage_contract.assert_called_once_with(result, 'document')


def test_signed_adapter_refuses_changed_scope_before_staging():
    scope, _, envelope, params, result, ctx = _fixture()
    calls = 0
    class Admission:
        def admit_signed(self, _ctx, _candidate):
            nonlocal calls
            calls += 1
            return scope if calls == 1 else (uuid4(), scope[1], scope[2])
    artifacts = Mock()
    artifacts.params.return_value = params
    artifacts.contract_bytes.return_value = canonical_bytes(envelope)
    with patch('photo_worker.operations.delivery.signed_adapter.assemble_signed_delivery',
               return_value=result):
        with pytest.raises(OperationFailure):
            execute_signed_assembly(ctx, artifacts,
                SignedDeliveryInstallation(admission=Admission()))
    artifacts.stage_contract.assert_not_called()


def test_signed_sql_admission_requires_exact_worker_scope():
    scope, _, _, params, _, ctx = _fixture()
    ctx.lease.command.tenant_id = scope[0]
    ctx.lease.command.run_id = scope[2]
    ctx.lease.command.task_id = uuid4()
    ctx.lease.command.params = SimpleNamespace(sha256='a' * 64)
    source = {'tenant_id': str(scope[0]), 'actor_id': str(scope[1]),
              'parent_id': str(scope[2]), 'task_id': str(ctx.lease.command.task_id),
              'parent_task_id': str(uuid4())}
    database = SimpleNamespace(delivery_signed_source_scope=Mock(return_value=source))
    admission = SQLSignedDeliveryAdmission(database)
    assert admission.admit_signed(ctx, params) == scope
    source['task_id'] = str(uuid4())
    with pytest.raises(OperationFailure):
        admission.admit_signed(ctx, params)


def test_delivery_registry_routes_only_declared_signed_root_to_signed_adapter():
    signed = SignedDeliveryInstallation(admission=SimpleNamespace(admit_signed=lambda *_: None))
    installation = DeliveryInstallation(admission=SimpleNamespace(admit=lambda *_: None), signed=signed)
    operations = delivery_operations(object(), installation)
    assemble = next(operation for operation in operations if operation.name == 'v2.delivery.assemble')
    artifacts = Mock()
    session = MagicMock()
    session.__enter__.return_value = artifacts
    signed_root = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    with patch('photo_worker.operations.delivery.adapter.TaskArtifacts.create', return_value=session), \
         patch('photo_worker.operations.delivery.adapter.execute_signed_assembly', return_value='signed') as signed_run, \
         patch('photo_worker.operations.delivery.adapter.execute', return_value='ordinary') as ordinary_run:
        artifacts.declared_contract_refs.return_value = (signed_root,)
        assert assemble.execute(object()) == 'signed'
        signed_run.assert_called_once()
        ordinary_run.assert_not_called()
        artifacts.declared_contract_refs.return_value = ()
        assert assemble.execute(object()) == 'ordinary'
        ordinary_run.assert_called_once()
