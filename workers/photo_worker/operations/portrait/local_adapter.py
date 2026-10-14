"""V28 CPU adapter; deployment must inject live version/mask authority."""
from contextlib import ExitStack
from typing import Protocol

from photo_worker.contracts.base import canonical_hash, canonical_bytes
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.executor import OperationFailure, OperationResult
from .local_inputs import verify_local_inputs
from .local_resources import admit_local_portrait
from .prepare_local import prepare_local
from .local_result import stage_local_result


class LocalPortraitAdmission(Protocol):
    def admit(self, ctx, params, source, target, protected) -> None:
        """Verify live tenant/version/source and immutable mask producer lineage.

        Task input declaration and matching geometry alone are insufficient.
        Implementations must refuse revoked/missing authority and recheck pins.
        """
        ...


def execute(ctx, artifacts, admission: LocalPortraitAdmission | None):
    if admission is None:
        raise OperationFailure('PORTRAIT_INPUT_AUTHORITY_UNAVAILABLE')
    operation = ctx.lease.command.operation
    model = {'portrait.hair':PortraitHairParams,'portrait.clothing':PortraitClothingParams}.get(operation)
    if (model is None or ctx.lease.command.operation_version!='1'
            or ctx.lease.command.resource_class!='cpu'
            or ctx.lease.execution_profile.get('renderer_version')!='v2.2'
            or ctx.lease.execution_profile.get('backend')!='cpu'):
        raise OperationFailure('CONFIG_INVALID')
    ctx.check_active()
    try:
        params = artifacts.params(model)
        if len(canonical_bytes(params)) > 65536 or canonical_hash(params) != ctx.lease.command.params.sha256:
            raise ValueError("PORTRAIT_PARAMS_BINDING_MISMATCH")
        source = artifacts.contract(params.base.source,WorkingImage)
        target = artifacts.contract(params.mask,MaskRef)
        protected = artifacts.contract(params.protected,MaskRef)
        verify_local_inputs(params,source,target,protected,ctx.lease.command.inputs)
        h,w = source.canvas.height,source.canvas.width
        admit_local_portrait(ctx.lease.execution_profile,w,h)
        ctx.check_active()
        admission.admit(ctx,params,source,target,protected)
        ctx.check_active()
        with ExitStack() as stack:
            def mapped(ref,shape,unit=False):
                path=stack.enter_context(artifacts.input(ref,suffix='.npy')).path
                return stack.enter_context(read_array(path,shape,'float32',ctx.check_active,unit_interval=unit))
            rgb=mapped(source.pixels,(h,w,3))
            mask=mapped(target.artifact,(h,w),True)
            guard=mapped(protected.artifact,(h,w),True)
            if source.alpha is not None:
                path=stack.enter_context(artifacts.input(source.alpha,suffix='.npy')).path
                stack.enter_context(read_alpha_array(path,(h,w),ctx.check_active))
            with prepare_local(params,rgb,mask,guard,source.color,artifacts.scratch,
                               ctx.lease.execution_profile,check_active=ctx.check_active) as files:
                ctx.check_active()
                admission.admit(ctx,params,source,target,protected)
                ctx.check_active()
                staged=stage_local_result(artifacts,params,source,files)
        ctx.check_active()
        return OperationResult(outputs=[staged.document],review_required=staged.review_required,
                               bytes_written=staged.bytes_written)
    except ValueError as exc:
        code=str(exc)
        raise OperationFailure(code if code in {'INVALID_LUMINANCE','PORTRAIT_COLOR_UNSUPPORTED'} else 'INPUT_INVALID') from exc


def local_portrait_operations(config, admission_factory=None):
    """Install only with a concrete operator-configured authority factory.

    This is an integration hook, not a default authority implementation. The
    factory receives attempt-scoped artifacts and must provide live relational
    and immutable producer checks. Runtime startup must verify its installation.
    """
    if admission_factory is None:
        return []
    if not callable(admission_factory):
        raise ValueError('PORTRAIT_ADMISSION_CONFIG_INVALID')
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.executor import Operation
    roles=frozenset({'document','working_image','pixels','report','mask','mask_pixels'})
    def run(ctx):
        with TaskArtifacts.create(ctx,config,roles) as artifacts:
            admission=admission_factory(ctx,artifacts)
            return execute(ctx,artifacts,admission)
    return [Operation('portrait.hair','cpu',run),Operation('portrait.clothing','cpu',run)]
