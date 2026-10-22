"""Canonical task and live SQL authority are both required to publish a seal."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import OperationFailure


class SQLEditorEffectsSealAdmission:
    def __init__(self, database):
        self.database = database

    def admit(self, ctx, params):
        ctx.check_active()
        lease = ctx.lease
        command = lease.command
        if (command.operation != 'editor.effects_seal' or command.operation_version != '1'
                or command.resource_class != 'cpu' or lease.execution_profile.get('backend') != 'cpu'):
            raise OperationFailure('EDITOR_EFFECTS_SEAL_SCOPE_STALE')
        raw = canonical_bytes(params)
        if len(raw) > 2097152 or sha256(raw).hexdigest() != command.params.sha256:
            raise OperationFailure('INPUT_INVALID')
        if self.database.admit_editor_effects_seal(lease, raw) is not True:
            raise OperationFailure('EDITOR_EFFECTS_SEAL_SCOPE_STALE')
        ctx.check_active()
