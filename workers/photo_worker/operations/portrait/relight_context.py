"""Local relight dependency view from the verified V2 closure, not transport roots."""
from copy import copy
from dataclasses import is_dataclass,replace
from photo_worker.contracts.base import canonical_hash
from photo_worker.runtime.executor import OperationFailure
from .local_manual_admission import _attempt_pin


def verified_relight_context(ctx,artifacts):
    """Keep gateway/heartbeat command intact; expand only local authority view."""
    ctx.check_active()
    if ctx.lease.execution_profile.get('renderer_version')!='v2.2':
        raise OperationFailure('RELIGHT_INPUT_CLOSURE_REQUIRED')
    refs=tuple(artifacts.declared_input_refs(maximum=512))
    if not refs or ctx.lease.command.params not in refs:
        raise OperationFailure('RELIGHT_INPUT_CLOSURE_INVALID')
    def identity():
        return (_attempt_pin(ctx),tuple(getattr(ctx.lease.command,key,None) for key in ('operation','operation_version','resource_class')),
                canonical_hash([ref.model_dump(mode='json') for ref in ctx.lease.command.inputs]))
    frozen=identity()
    command=ctx.lease.command
    if callable(getattr(command,'model_copy',None)):
        command=command.model_copy(update={'inputs':list(refs)},deep=True)
    else:
        command=copy(command);command.inputs=list(refs)
    if is_dataclass(ctx.lease):lease=replace(ctx.lease,command=command)
    else:
        lease=copy(ctx.lease);lease.command=command
    class View:
        def __init__(self):self.lease=lease
        def __getattr__(self,name):return getattr(ctx,name)
        def check_active(self):
            ctx.check_active()
            if identity()!=frozen:
                raise OperationFailure('RELIGHT_TRANSPORT_CHANGED')
    result=View();result.check_active();return result
