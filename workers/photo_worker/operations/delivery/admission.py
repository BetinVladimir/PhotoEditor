"""Combine real SQL lease checks with mandatory frozen-source authority."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.delivery import V2DeliveryAssembleParams, V2DeliveryPrepareParams
from photo_worker.runtime.executor import OperationFailure


class SQLDeliveryAdmission:
    def __init__(self, database, verify_source=None):
        if verify_source is None and callable(getattr(database, 'delivery_source_ready', None)):
            verify_source = lambda ctx, params: database.delivery_source_ready(ctx.lease, canonical_bytes(params))
        if not callable(getattr(database, 'delivery_lease_ready', None)) or not callable(verify_source):
            raise ValueError('DELIVERY_ADMISSION_INVALID')
        self.database, self.verify_source = database, verify_source

    def admit(self, ctx, params):
        ctx.check_active()
        command = ctx.lease.command
        expected = {'v2.delivery.prepare': V2DeliveryPrepareParams,
                    'v2.delivery.assemble': V2DeliveryAssembleParams}.get(command.operation)
        profile = ctx.lease.execution_profile
        if (expected is None or not isinstance(params, expected) or not isinstance(profile, dict) or command.operation_version != '1' or
                command.resource_class != 'cpu' or profile.get('renderer_version') != 'v2.2' or
                profile.get('backend') != 'cpu'):
            raise OperationFailure('DELIVERY_SCOPE_STALE')
        owned_params = params.model_copy(deep=True)
        raw = canonical_bytes(owned_params)
        if len(raw) > 65536 or sha256(raw).hexdigest() != command.params.sha256:
            raise OperationFailure('INPUT_INVALID')
        if self.database.delivery_lease_ready(ctx.lease, raw) is not True:
            raise OperationFailure('DELIVERY_SCOPE_STALE')
        ctx.check_active()
        if canonical_bytes(params) != raw:
            raise OperationFailure('INPUT_INVALID')
        # A live task is insufficient: sources must authorize the same params bytes.
        source_params = owned_params.model_copy(deep=True)
        if self.verify_source(ctx, source_params) is not True:
            raise OperationFailure('DELIVERY_SOURCE_STALE')
        ctx.check_active()
        if canonical_bytes(params) != raw or canonical_bytes(source_params) != raw:
            raise OperationFailure('INPUT_INVALID')
