"""Live frozen-origin and canonical lease proof before original-layer IO."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import OperationFailure

class SQLEditorLayersPrecomposeAdmission:
    def __init__(self,database):self.database=database
    def admit(self,ctx,params):
        ctx.check_active();lease=ctx.lease;command=lease.command;raw=canonical_bytes(params)
        if (command.operation!='editor.layers_precompose' or command.operation_version!='1'
                or command.resource_class!='cpu' or lease.execution_profile.get('backend')!='cpu'
                or sha256(raw).hexdigest()!=command.params.sha256
                or self.database.admit_editor_layers_precompose(lease,raw) is not True):
            raise OperationFailure('EDITOR_PRECOMPOSE_SCOPE_STALE')
        ctx.check_active()
