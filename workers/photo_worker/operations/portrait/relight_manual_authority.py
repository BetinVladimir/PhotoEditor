"""Replay one manual relight mask with its original producer companion.

A selected guard may accompany a person subject. Its original manual target is
still required for vector replay; it need not be the relight subject. The origin
resolver must enforce current SQL lease/source/canonical-producer authority.
"""
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.manual_mask import RasterizedMask
from photo_worker.runtime.executor import OperationFailure
from .local_manual_admission import _attempt_pin
from .local_manual_inputs import ManualPairOrigin,read_verified_manual_pair


class RelightManualMaskAuthority:
    def __init__(self,artifacts,origin_authority):
        if (not callable(getattr(artifacts,'contract',None))
                or not callable(getattr(origin_authority,'resolve',None))):
            raise ValueError('RELIGHT_MANUAL_AUTHORITY_INVALID')
        self.artifacts,self.authority=artifacts,origin_authority

    def verify(self,ctx,params,source,mask,*,protected):
        ctx.check_active()
        def identity():
            return (_attempt_pin(ctx),canonical_hash([ref.model_dump(mode='json') for ref in ctx.lease.command.inputs]))
        attempt=identity()
        if type(protected) is not bool or mask.selector!='manual' or mask.protected is not protected:
            raise OperationFailure('RELIGHT_MANUAL_MASK_INVALID')
        if canonical_hash(params)!=ctx.lease.command.params.sha256:
            raise OperationFailure('RELIGHT_PARAMS_BINDING_MISMATCH')
        expected=params.protected if protected else params.subject
        if canonical_hash(mask)!=expected.sha256 or expected not in ctx.lease.command.inputs:
            raise OperationFailure('RELIGHT_MANUAL_MASK_BINDING_MISMATCH')
        def resolve():
            ctx.check_active()
            origin=self.authority.resolve(ctx,params,source,mask,protected=protected)
            ctx.check_active()
            if identity()!=attempt:raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
            if not isinstance(origin,ManualPairOrigin):raise OperationFailure('RELIGHT_MANUAL_ORIGIN_INVALID')
            return origin
        origin=resolve()
        if origin.root not in ctx.lease.command.inputs:
            raise OperationFailure('RELIGHT_MANUAL_INPUT_UNDECLARED')
        root=self.artifacts.contract(origin.root,RasterizedMask)
        if not isinstance(root,RasterizedMask) or canonical_hash(root)!=origin.root.sha256:
            raise OperationFailure('RELIGHT_MANUAL_ROOT_MISMATCH')
        selected=root.protected if protected else root.target
        if canonical_bytes(selected)!=canonical_bytes(mask):
            raise OperationFailure('RELIGHT_MANUAL_ROOT_MISMATCH')
        read_verified_manual_pair(ctx,self.artifacts,params.base.source,source,
                                  root.target,root.protected,origin)
        if resolve()!=origin:raise OperationFailure('RELIGHT_MANUAL_ORIGIN_CHANGED')
        return True


class SQLRelightManualOrigin:
    """Discover canonical root, then recheck its children before granting origin."""
    def __init__(self,database,artifacts):
        if not callable(getattr(database,'relight_manual_origin',None)) or not callable(getattr(artifacts,'contract_bytes',None)):
            raise ValueError('RELIGHT_MANUAL_AUTHORITY_INVALID')
        self.database,self.artifacts=database,artifacts

    def resolve(self,ctx,params,source,mask,*,protected):
        from photo_worker.contracts.base import ArtifactRef,parse_json
        from photo_worker.contracts.manual_mask import MaskRasterParams
        from hashlib import sha256
        ctx.check_active();attempt=_attempt_pin(ctx)
        body,mask_body=canonical_bytes(params),canonical_bytes(mask)
        if canonical_hash(params)!=ctx.lease.command.params.sha256:
            raise OperationFailure('RELIGHT_PARAMS_BINDING_MISMATCH')
        def lookup(root):
            ctx.check_active()
            value=self.database.relight_manual_origin(ctx.lease,body,mask_body,root,protected)
            ctx.check_active()
            if _attempt_pin(ctx)!=attempt:raise OperationFailure('RELIGHT_ATTEMPT_CHANGED')
            if not isinstance(value,dict) or set(value)!={'root','params'}:
                raise OperationFailure('RELIGHT_MANUAL_ORIGIN_INVALID')
            return value
        record=lookup(None)
        root_ref=ArtifactRef.model_validate_json(canonical_bytes(record['root']));params_ref=ArtifactRef.model_validate_json(canonical_bytes(record['params']))
        if root_ref not in ctx.lease.command.inputs or params_ref not in ctx.lease.command.inputs:
            raise OperationFailure('RELIGHT_MANUAL_INPUT_UNDECLARED')
        root_raw=self.artifacts.contract_bytes(root_ref,RasterizedMask)
        params_raw=self.artifacts.contract_bytes(params_ref,MaskRasterParams)
        for raw,ref in ((root_raw,root_ref),(params_raw,params_ref)):
            if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=ref.sha256:
                raise OperationFailure('RELIGHT_MANUAL_DOCUMENT_MISMATCH')
            parse_json(raw,max_bytes=1048576)
        root=RasterizedMask.model_validate_json(root_raw)
        original=MaskRasterParams.model_validate_json(params_raw)
        if original.image!=params.base.source or root.image!=params.base.source:
            raise OperationFailure('RELIGHT_MANUAL_SOURCE_MISMATCH')
        if original.vectors is not None and original.vectors not in ctx.lease.command.inputs:
            raise OperationFailure('RELIGHT_MANUAL_INPUT_UNDECLARED')
        if lookup(root_raw)!=record:raise OperationFailure('RELIGHT_MANUAL_ORIGIN_CHANGED')
        return ManualPairOrigin(root_ref,params_ref,original.vectors)
