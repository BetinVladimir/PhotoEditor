"""Leased V30 adapters. Relational admission and preset semantics are mandatory."""
from dataclasses import dataclass
from typing import Callable, Protocol
from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from photo_worker.contracts.delivery import (DeliveryAssemblyInput, DeliverySlotShard,
    FrozenDeliveryInput, V2DeliveryAssembleParams, V2DeliveryPrepareParams)
from photo_worker.operations.session.contracts import InputSnapshot
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.contracts.export_preset import FrozenExportPresetConfig, validate_frozen_export_preset
from .assembly import assemble_delivery
from .preparation import prepare_delivery
from .signed_adapter import SignedDeliveryInstallation, execute_signed_assembly


class DeliveryAdmission(Protocol):
    def admit(self, ctx, params) -> None:
        """Check current tenant/project/run/task fence, immutable input and closure.

        For assembly require all frozen child exports terminal, physical ready
        exports retained, and exact snapshot/preset inventory under authority.
        Refuse revoked/expired inputs; never infer authority from JSON states.
        """
        ...


@dataclass(frozen=True)
class DeliveryInstallation:
    admission: DeliveryAdmission
    preset_model: type[Contract] = FrozenExportPresetConfig
    validate_preset: Callable[[bytes], None] = validate_frozen_export_preset
    signed: SignedDeliveryInstallation | None = None


def execute(ctx, artifacts, installation: DeliveryInstallation, *, operation: str):
    profile, command = ctx.lease.execution_profile, ctx.lease.command
    if (not isinstance(profile, dict) or profile.get('renderer_version') != 'v2.2' or profile.get('backend') != 'cpu' or
            command.resource_class != 'cpu' or command.operation_version != '1' or
            command.operation != operation):
        raise OperationFailure('CONFIG_INVALID')
    if installation is None or not callable(getattr(installation.admission, 'admit', None)):
        raise OperationFailure('DELIVERY_ADMISSION_UNAVAILABLE')

    def active():
        ctx.check_active()
        return False

    def admit(params):
        active()
        candidate = params.model_copy(deep=True)
        expected = canonical_bytes(candidate)
        installation.admission.admit(ctx, candidate)
        active()
        if canonical_bytes(candidate) != expected:
            raise OperationFailure('INPUT_INVALID')

    try:
        active()
        if operation == 'v2.delivery.assemble':
            params = artifacts.params(V2DeliveryAssembleParams).model_copy(deep=True)
            admit(params)
            raw = artifacts.contract_bytes(params.assembly.model_copy(deep=True), DeliveryAssemblyInput)
            active()
            result = assemble_delivery(params.assembly, raw,
                lambda ref: artifacts.contract_bytes(ref, DeliverySlotShard), active)
        elif operation == 'v2.delivery.prepare':
            params = artifacts.params(V2DeliveryPrepareParams).model_copy(deep=True)
            admit(params)
            raw = artifacts.contract_bytes(params.input.model_copy(deep=True), FrozenDeliveryInput)
            active()
            frozen = FrozenDeliveryInput.model_validate_json(raw)
            configs = {(p.config.artifact_id, p.config.sha256) for p in frozen.presets}

            def read_object(ref):
                active()
                if ref == frozen.snapshot:
                    return artifacts.contract_bytes(ref, InputSnapshot)
                if (ref.artifact_id, ref.sha256) not in configs:
                    raise ValueError('DELIVERY_UNDECLARED_CONFIG')
                return artifacts.contract_bytes(ref, installation.preset_model)

            result = prepare_delivery(params.input, raw, read_object,
                                      installation.validate_preset, active)
        else:
            raise OperationFailure('CONFIG_INVALID')
        active()
        admit(params)
        active()
        size = len(canonical_bytes(result))
        active()
        root = artifacts.stage_contract(result, 'document')
        active()
        # Human publication acceptance belongs to the package transaction,
        # not to a worker asking the scheduler to review its JSON output.
        return OperationResult(outputs=[root], review_required=False, bytes_written=size)
    except ValueError as error:
        raise OperationFailure('INPUT_INVALID') from error


def delivery_operations(config, installation: DeliveryInstallation):
    if (not isinstance(installation, DeliveryInstallation) or
            not callable(getattr(installation.admission, 'admit', None)) or
            not isinstance(installation.preset_model, type) or
            not issubclass(installation.preset_model, Contract) or
            not callable(installation.validate_preset) or
            installation.signed is not None and (
                not isinstance(installation.signed, SignedDeliveryInstallation) or
                not callable(getattr(installation.signed.admission, 'admit_signed', None)))):
        raise ValueError('DELIVERY_INSTALLATION_INVALID')
    def build(operation):
        def run(ctx):
            with TaskArtifacts.create(ctx, config, frozenset({'document'})) as artifacts:
                if operation == 'v2.delivery.assemble':
                    signed_roots = artifacts.declared_contract_refs('SignedDeliveryAssemblyEnvelope', 1)
                    if signed_roots:
                        if installation.signed is None:
                            raise OperationFailure('DELIVERY_SIGNED_ADMISSION_UNAVAILABLE')
                        return execute_signed_assembly(ctx, artifacts, installation.signed)
                return execute(ctx, artifacts, installation, operation=operation)
        return Operation(operation, 'cpu', run)
    return [build('v2.delivery.prepare'), build('v2.delivery.assemble')]
