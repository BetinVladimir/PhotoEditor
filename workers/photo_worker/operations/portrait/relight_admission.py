"""Attempt-bound composition of live producer authority and exact qualification."""
from photo_worker.contracts.base import canonical_hash, canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.relight_qualification import verify_relight_qualification
from .local_manual_admission import _attempt_pin
from .relight_inputs import verify_relight_inputs


class QualifiedRelightAdmission:
    """Resolver must supply live authorized (pin,report bytes,calibration bytes,policy).

    Resolver owns SQL lease/tenant/version/producer/object-version authority.
    This composition never substitutes document validity for those checks.
    Each admission resolves twice and re-verifies original evidence; no cached
    approval is reused across preparation or publication.
    """
    def __init__(self,resolver,expected_runtime_sha):
        if not callable(getattr(resolver,'resolve',None)):
            raise ValueError('RELIGHT_AUTHORITY_INVALID')
        self.resolver=resolver
        self.runtime_sha=expected_runtime_sha
        self._identity=None
        self._context=None

    def admit(self,ctx,params,source,depth,subject,protected,calibration):
        ctx.check_active()
        def attempt_identity():
            command=ctx.lease.command
            if (command.operation!='image.relight' or command.operation_version!='1'
                    or command.resource_class!='cpu'
                    or ctx.lease.execution_profile.get('relight_runtime_sha')!=self.runtime_sha):
                raise OperationFailure('RELIGHT_ATTEMPT_INVALID')
            return (_attempt_pin(ctx),canonical_hash({
                'operation':command.operation,'version':command.operation_version,
                'resource_class':command.resource_class,
                'inputs':[ref.model_dump(mode='json') for ref in command.inputs]}))
        attempt=attempt_identity()
        if canonical_hash(params)!=ctx.lease.command.params.sha256:
            raise OperationFailure('RELIGHT_PARAMS_BINDING_MISMATCH')
        verify_relight_inputs(params,source,depth,subject,protected,calibration,ctx.lease.command.inputs)
        identity=None
        for _ in range(2):
            ctx.check_active()
            evidence=self.resolver.resolve(ctx,params,source,depth,subject,protected,calibration)
            if not isinstance(evidence,tuple) or len(evidence)!=4:
                raise OperationFailure('RELIGHT_QUALIFICATION_INVALID')
            pin,actual=verify_relight_qualification(*evidence,params=params,
                expected_runtime_sha=self.runtime_sha,check_active=ctx.check_active)
            if actual!=calibration:
                raise OperationFailure('RELIGHT_CALIBRATION_MISMATCH')
            current=(attempt,pin.fingerprint())
            if (identity is not None and identity!=current) or (
                    self._identity is not None and (self._context is not ctx or self._identity!=current)):
                raise OperationFailure('RELIGHT_AUTHORITY_CHANGED')
            if attempt_identity()!=attempt:
                raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
            identity=current
        self._identity=identity
        self._context=ctx


class LeaseGatedRelightResolver:
    """Compose real SQL attempt gate with independent producer/evidence resolver."""
    def __init__(self,database,resolver):
        if (not callable(getattr(database,'relight_lease_ready',None))
                or not callable(getattr(database,'relight_source_ready',None))
                or not callable(getattr(resolver,'resolve',None))):
            raise ValueError('RELIGHT_AUTHORITY_INVALID')
        self.database,self.resolver=database,resolver

    def resolve(self,ctx,params,*documents):
        ctx.check_active()
        pin=_attempt_pin(ctx)
        body=canonical_bytes(params)
        if len(body)>65536 or canonical_hash(params)!=ctx.lease.command.params.sha256:
            raise OperationFailure('RELIGHT_PARAMS_BINDING_MISMATCH')
        def live():
            ctx.check_active()
            if _attempt_pin(ctx)!=pin:raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
            if self.database.relight_lease_ready(ctx.lease,body) is not True:
                raise OperationFailure('RELIGHT_LEASE_NOT_READY')
            if self.database.relight_source_ready(ctx.lease,body) is not True:
                raise OperationFailure('RELIGHT_SOURCE_NOT_READY')
            ctx.check_active()
            if _attempt_pin(ctx)!=pin:raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
        live()
        evidence=self.resolver.resolve(ctx,params,*documents)
        live()
        return evidence
