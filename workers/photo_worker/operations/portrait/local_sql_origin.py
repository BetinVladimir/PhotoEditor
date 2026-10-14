"""Live SQL canonical manual producer resolution, followed by pinned Params read."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.manual_mask import MaskRasterParams
from photo_worker.runtime.executor import OperationFailure
from .local_manual_admission import _attempt_pin
from .local_manual_inputs import ManualPairOrigin


class SQLManualPairAuthority:
    def __init__(self,database,artifacts):
        if (not callable(getattr(database,'local_portrait_manual_origin',None))
                or not callable(getattr(artifacts,'contract_bytes',None))):
            raise ValueError('PORTRAIT_MANUAL_ADMISSION_CONFIG_INVALID')
        self.database,self.artifacts=database,artifacts

    def resolve(self,ctx,params,source,target,protected):
        ctx.check_active()
        pin=_attempt_pin(ctx)
        body=canonical_bytes(params)
        if len(body)>65536 or sha256(body).hexdigest()!=ctx.lease.command.params.sha256:
            raise OperationFailure('PORTRAIT_PARAMS_BINDING_MISMATCH')
        def lookup(vectors=None):
            ctx.check_active()
            if _attempt_pin(ctx)!=pin:raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_MISMATCH')
            row=self.database.local_portrait_manual_origin(ctx.lease,body,target.artifact,protected.artifact,vectors)
            ctx.check_active()
            if _attempt_pin(ctx)!=pin:raise OperationFailure('PORTRAIT_MANUAL_ATTEMPT_MISMATCH')
            try:
                if not isinstance(row,(tuple,list)) or len(row)!=4:raise ValueError('origin')
                root=ArtifactRef(artifact_id=row[0],sha256=row[1])
                original=ArtifactRef(artifact_id=row[2],sha256=row[3])
            except (ValueError,TypeError) as error:
                raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_INVALID') from error
            if (root.artifact_id.int==0 or original.artifact_id.int==0 or root.artifact_id==original.artifact_id
                    or root not in ctx.lease.command.inputs or original not in ctx.lease.command.inputs):
                raise OperationFailure('PORTRAIT_MANUAL_INPUT_UNDECLARED')
            return root,original
        root,original=lookup()
        raw=self.artifacts.contract_bytes(original,MaskRasterParams)
        ctx.check_active()
        if not isinstance(raw,bytes) or len(raw)>1048576 or sha256(raw).hexdigest()!=original.sha256:
            raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_INVALID')
        producer=MaskRasterParams.model_validate_json(raw)
        if producer.image!=params.base.source:
            raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_INVALID')
        if producer.vectors is not None and producer.vectors not in ctx.lease.command.inputs:
            raise OperationFailure('PORTRAIT_MANUAL_INPUT_UNDECLARED')
        if lookup(producer.vectors)!=(root,original):
            raise OperationFailure('PORTRAIT_MANUAL_ORIGIN_CHANGED')
        return ManualPairOrigin(root,original,producer.vectors)


def sql_manual_local_portrait_operations(config,database,runtime_lock,expected_runtime_sha):
    """Explicit installation after SQL rollout and hardware/runtime qualification."""
    from .local_manual_admission import manual_local_portrait_operations
    return manual_local_portrait_operations(config,database,
        lambda ctx,artifacts:SQLManualPairAuthority(database,artifacts),runtime_lock,expected_runtime_sha)
