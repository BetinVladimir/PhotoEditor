"""Private leased adapter for required-provenance Delivery assembly."""
from dataclasses import dataclass
from typing import Protocol
from uuid import UUID

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.delivery import (
    DeliveryAssemblyInput, DeliverySlotShard, SignedDeliveryAssemblyClosure,
    SignedDeliveryAssemblyEnvelope, V2DeliveryAssembleParams,
)
from photo_worker.runtime.executor import OperationFailure, OperationResult
from .assembly import assemble_signed_delivery


class SignedDeliveryAdmission(Protocol):
    def admit_signed(self, ctx, params: V2DeliveryAssembleParams) -> tuple[UUID, UUID, UUID]:
        """Return current SQL-authorized tenant, actor and parent identities.

        The implementation must verify the task fence, complete signed input
        closure, frozen policy, V31 receipts and exact physical versions.
        """


@dataclass(frozen=True)
class SignedDeliveryInstallation:
    admission: SignedDeliveryAdmission


class SQLSignedDeliveryAdmission:
    """Translate one fenced SQL source scope into trusted adapter identities."""
    def __init__(self, database):
        if not callable(getattr(database, 'delivery_signed_source_scope', None)):
            raise ValueError('DELIVERY_SIGNED_ADMISSION_INVALID')
        self.database = database

    def admit_signed(self, ctx, params: V2DeliveryAssembleParams) -> tuple[UUID, UUID, UUID]:
        ctx.check_active()
        command = ctx.lease.command
        raw = canonical_bytes(params.model_copy(deep=True))
        if len(raw) > 65536:
            raise OperationFailure('INPUT_INVALID')
        source = self.database.delivery_signed_source_scope(ctx.lease, raw)
        ctx.check_active()
        if not isinstance(source, dict) or set(source) != {
                'tenant_id', 'actor_id', 'parent_id', 'task_id', 'parent_task_id'}:
            raise OperationFailure('DELIVERY_SIGNED_SCOPE_STALE')
        try:
            tenant, actor, parent, task, parent_task = (
                UUID(str(source[key])) for key in
                ('tenant_id', 'actor_id', 'parent_id', 'task_id', 'parent_task_id'))
        except (ValueError, TypeError, AttributeError) as error:
            raise OperationFailure('DELIVERY_SIGNED_SCOPE_STALE') from error
        if (tenant != command.tenant_id or parent != command.run_id or task != command.task_id
                or actor.int == 0 or parent_task.int == 0):
            raise OperationFailure('DELIVERY_SIGNED_SCOPE_STALE')
        return tenant, actor, parent


def execute_signed_assembly(ctx, artifacts, installation: SignedDeliveryInstallation):
    """Stage one v2 signed manifest after two independent authority checks.

    This path is deliberately separate from the off-mode adapter and remains
    unregistered until the corresponding signed SQL source gate is installed.
    """
    profile, command = ctx.lease.execution_profile, ctx.lease.command
    if (not isinstance(profile, dict) or profile.get('renderer_version') != 'v2.2'
            or profile.get('backend') != 'cpu' or command.resource_class != 'cpu'
            or command.operation != 'v2.delivery.assemble'
            or command.operation_version != '1'):
        raise OperationFailure('CONFIG_INVALID')
    if installation is None or not callable(getattr(installation.admission, 'admit_signed', None)):
        raise OperationFailure('DELIVERY_SIGNED_ADMISSION_UNAVAILABLE')

    def active():
        ctx.check_active()
        return False

    def admit(params):
        active()
        before = canonical_bytes(params)
        scope = installation.admission.admit_signed(ctx, params.model_copy(deep=True))
        active()
        if (canonical_bytes(params) != before or not isinstance(scope, tuple) or len(scope) != 3
                or any(not isinstance(value, UUID) or value.int == 0 for value in scope)):
            raise OperationFailure('DELIVERY_SIGNED_SCOPE_STALE')
        return scope

    try:
        active()
        params = artifacts.params(V2DeliveryAssembleParams).model_copy(deep=True)
        tenant, actor, parent = admit(params)
        envelope_raw = artifacts.contract_bytes(params.assembly.model_copy(deep=True),
                                                SignedDeliveryAssemblyEnvelope)
        active()
        envelope = SignedDeliveryAssemblyEnvelope.model_validate_json(envelope_raw)

        def read_document(ref: ArtifactRef):
            active()
            if ref == envelope.assembly:
                model = DeliveryAssemblyInput
            elif ref == envelope.signing_closure:
                model = SignedDeliveryAssemblyClosure
            else:
                model = DeliverySlotShard
            return artifacts.contract_bytes(ref.model_copy(deep=True), model)

        result = assemble_signed_delivery(params.assembly, envelope_raw, read_document,
                                          tenant, actor, parent, active)
        active()
        if admit(params) != (tenant, actor, parent):
            raise OperationFailure('DELIVERY_SIGNED_SCOPE_STALE')
        active()
        size = len(canonical_bytes(result))
        root = artifacts.stage_contract(result, 'document')
        active()
        return OperationResult(outputs=[root], review_required=False, bytes_written=size)
    except ValueError as error:
        raise OperationFailure('INPUT_INVALID') from error
