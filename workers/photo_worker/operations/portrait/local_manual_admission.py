"""Manual-only V28 admission; live relational authority is mandatory."""
from typing import Protocol
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from hashlib import sha256

from photo_worker.runtime.executor import OperationFailure
from .local_inputs import verify_local_inputs
from .local_manual_inputs import ManualPairOrigin, read_verified_manual_pair


class ManualPairAuthority(Protocol):
    def resolve(self, ctx, params, source, target, protected) -> ManualPairOrigin:
        """Recheck current lease, tenant/version and canonical producer pins.

        Resolve only the exact declared original root/Params/vector inputs.
        Caller-provided locators or structural geometry are not authority.
        """
        ...


def _attempt_pin(ctx):
    lease=ctx.lease
    command=lease.command
    ids=tuple(getattr(command,key,None) for key in ('tenant_id','run_id','task_id'))+(getattr(lease,'worker_id',None),)
    attempt=getattr(command,'attempt',None)
    fence=getattr(lease,'fence',None)
    params=getattr(command,'params',None)
    digest=getattr(lease,'input_digest',None)
    profile=getattr(lease,'execution_profile',None)
    if (any(not isinstance(value,UUID) or value.int==0 for value in ids)
            or type(attempt) is not int or attempt<1 or type(fence) is not int or fence<1
            or not isinstance(params,ArtifactRef) or params.artifact_id.int==0 or not isinstance(digest,str)
            or len(digest)!=64 or any(c not in '0123456789abcdef' for c in digest)
            or not isinstance(profile,dict)):
        raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_INVALID')
    try:
        profile_bytes=canonical_bytes(profile)
    except (ValueError,TypeError) as error:
        raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_INVALID') from error
    if len(profile_bytes)>65536:
        raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_INVALID')
    return (*ids,attempt,fence,params,digest,sha256(profile_bytes).hexdigest())


class ManualLocalPortraitAdmission:
    def __init__(self, artifacts, authority: ManualPairAuthority):
        if artifacts is None or authority is None or not callable(getattr(authority,'resolve',None)):
            raise ValueError('PORTRAIT_MANUAL_ADMISSION_CONFIG_INVALID')
        self.artifacts=artifacts
        self.authority=authority
        self._origin=None
        self._context=None
        self._attempt=None

    def admit(self, ctx, params, source, target, protected):
        ctx.check_active()
        attempt=_attempt_pin(ctx)
        if self._context is not None and (self._context is not ctx or self._attempt!=attempt):
            raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_MISMATCH')
        verify_local_inputs(params,source,target,protected,ctx.lease.command.inputs)
        if target.selector!='manual' or protected.selector!='manual':
            raise OperationFailure('PORTRAIT_MANUAL_MASK_REQUIRED')
        origin=self.authority.resolve(ctx,params,source,target,protected)
        ctx.check_active()
        if not isinstance(origin,ManualPairOrigin):
            raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_INVALID')
        if self._origin is not None and origin!=self._origin:
            raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_CHANGED')
        read_verified_manual_pair(ctx,self.artifacts,params.base.source,source,target,protected,origin)
        ctx.check_active()
        # No cached approval: the adapter invokes admit again before output.
        # Repeat relational admission after physical replay to detect revocation.
        repeated=self.authority.resolve(ctx,params,source,target,protected)
        ctx.check_active()
        if _attempt_pin(ctx)!=attempt:
            raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_MISMATCH')
        if not isinstance(repeated,ManualPairOrigin) or repeated!=origin:
            raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_CHANGED')

        # Pin identity across prepare and publication, without caching approval.
        # Every call still performs both live checks and physical replay.
        self._origin=origin
        self._context=ctx
        self._attempt=attempt


class LeaseGatedManualPairAuthority:
    """Compose the concrete worker SQL lease gate with producer authority.

    producer_authority must independently resolve authorized original lineage.
    A live task lease alone never authorizes arbitrary mask documents.
    """
    def __init__(self, database, producer_authority):
        if (not callable(getattr(database,'local_portrait_lease_ready',None))
                or not callable(getattr(producer_authority,'resolve',None))):
            raise ValueError('PORTRAIT_MANUAL_ADMISSION_CONFIG_INVALID')
        self.database=database
        self.producer_authority=producer_authority

    def resolve(self, ctx, params, source, target, protected):
        ctx.check_active()
        pin=_attempt_pin(ctx)
        body=canonical_bytes(params)
        if len(body)>65536 or sha256(body).hexdigest()!=ctx.lease.command.params.sha256:
            raise OperationFailure('PORTRAIT_PARAMS_BINDING_MISMATCH')
        def live():
            ctx.check_active()
            if _attempt_pin(ctx)!=pin:
                raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_MISMATCH')
            if self.database.local_portrait_lease_ready(ctx.lease,body) is not True:
                raise OperationFailure('PORTRAIT_LEASE_NOT_READY')
            ctx.check_active()
            if _attempt_pin(ctx)!=pin:
                raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_MISMATCH')
        live()
        origin=self.producer_authority.resolve(ctx,params,source,target,protected)
        if not isinstance(origin,ManualPairOrigin):
            raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_INVALID')
        live()
        return origin


def manual_local_portrait_operations(config, database, producer_authority_factory, runtime_lock, expected_runtime_sha):
    """Install attempt-scoped lease+lineage admission; never infer authority."""
    if (not callable(getattr(database,'local_portrait_lease_ready',None))
            or not callable(producer_authority_factory)):
        raise ValueError('PORTRAIT_MANUAL_ADMISSION_CONFIG_INVALID')
    from .local_runtime_lock import verify_installed_runtime
    verify_installed_runtime(runtime_lock,expected_runtime_sha)
    from .local_adapter import local_portrait_operations
    def admission(ctx, artifacts):
        ctx.check_active()
        if ctx.lease.execution_profile.get('portrait_runtime_sha')!=expected_runtime_sha:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        verify_installed_runtime(runtime_lock,expected_runtime_sha)
        ctx.check_active()
        producer=producer_authority_factory(ctx,artifacts)
        return ManualLocalPortraitAdmission(artifacts,LeaseGatedManualPairAuthority(database,producer))
    return local_portrait_operations(config,admission)
