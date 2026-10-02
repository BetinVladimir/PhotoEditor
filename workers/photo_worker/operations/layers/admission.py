"""Relational admission for original-source composition of an adopted lens result.

No registration is implied: qualification and graph activation are separate gates.
"""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure
from .contracts import LayerComposeParams


class LensUserLayerAdmission:
    def __init__(self, database: TaskDatabase):
        self.database = database

    def admit(self, ctx, params: LayerComposeParams) -> None:
        ctx.check_active()
        lease = ctx.lease
        command = lease.command
        if (command.operation != 'layer.compose' or command.operation_version != '1'
                or command.resource_class != 'cpu' or params.source_result is not None
                or lease.execution_profile.get('renderer_version') != 'v2.2'
                or lease.execution_profile.get('backend') != 'cpu'):
            raise OperationFailure('LAYER_SCOPE_STALE')
        raw = canonical_bytes(params)
        if len(raw) > 65536 or sha256(raw).hexdigest() != command.params.sha256:
            raise OperationFailure('INPUT_INVALID')
        if not self.database.admit_lens_user_composition(lease, raw):
            raise OperationFailure('LAYER_SCOPE_STALE')
        ctx.check_active()
