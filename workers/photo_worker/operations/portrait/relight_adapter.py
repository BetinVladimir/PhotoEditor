"""CPU relight execution hook requiring independently installed live authority."""
from contextlib import ExitStack, contextmanager
from typing import Protocol
import numpy as np
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, RelativeDepth, MaskRef
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.session.contracts import Calibration
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.executor import OperationFailure, OperationResult
from .local_resources import admit_local_portrait
from .relight_inputs import verify_relight_inputs
from .prepare_relight import prepare_relight
from .relight_result import stage_relight_result


class RelightAdmission(Protocol):
    def admit(self, ctx, params, source, depth, subject, protected, calibration) -> None:
        """Recheck tenant/version, all producer pins and trusted qualification.

        Must bind the exact Calibration to live model/runtime/device qualification;
        document qualified=true and declared refs do not grant authority.
        """
        ...


@contextmanager
def _invalid_plane(values, shape, scratch, check_active):
    """Convert producer float32 0/1 invalid mask into bounded scratch bool NPY."""
    with scratch.reserve(shape[0]*shape[1]+8192) as reservation:
        path=reservation.path('relight-invalid','.npy')
        mapped=np.lib.format.open_memmap(path,mode='w+',dtype=np.bool_,shape=shape)
        try:
            for y in range(0,shape[0],256):
                for x in range(0,shape[1],512):
                    check_active();region=np.s_[y:y+256,x:x+512]
                    if values is None:
                        mapped[region]=False
                    else:
                        block=values[region]
                        if np.any((block!=0)&(block!=1)):
                            raise ValueError('RELIGHT_INVALID_MASK_NONBINARY')
                        mapped[region]=block!=0
            mapped.flush();reservation.validate();check_active()
            yield mapped
        finally:
            mapped._mmap.close()


def execute(ctx, artifacts, admission: RelightAdmission | None):
    if admission is None:
        raise OperationFailure('RELIGHT_INPUT_AUTHORITY_UNAVAILABLE')
    command=ctx.lease.command;profile=ctx.lease.execution_profile
    if (command.operation!='image.relight' or command.operation_version!='1'
            or command.resource_class!='cpu' or profile.get('backend')!='cpu'
            or profile.get('renderer_version')!='v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    ctx.check_active()
    try:
        params=artifacts.params(ImageRelightParams)
        if len(canonical_bytes(params))>65536 or canonical_hash(params)!=command.params.sha256:
            raise ValueError('RELIGHT_PARAMS_BINDING_MISMATCH')
        source=artifacts.contract(params.base.source,WorkingImage)
        depth=artifacts.contract(params.depth,RelativeDepth)
        subject=artifacts.contract(params.subject,MaskRef)
        protected=artifacts.contract(params.protected,MaskRef)
        calibration=artifacts.contract(params.calibration,Calibration)
        verify_relight_inputs(params,source,depth,subject,protected,calibration,command.inputs)
        h,w=source.canvas.height,source.canvas.width
        admit_local_portrait(profile,w,h)
        def authorize():
            ctx.check_active()
            admission.admit(ctx,params,source,depth,subject,protected,calibration)
            ctx.check_active()
        authorize()
        with ExitStack() as stack:
            def mapped(ref,shape,unit=False):
                path=stack.enter_context(artifacts.input(ref,suffix='.npy')).path
                return stack.enter_context(read_array(path,shape,'float32',ctx.check_active,unit_interval=unit))
            rgb=mapped(source.pixels,(h,w,3))
            values=mapped(depth.artifact,(h,w),True)
            subject_pixels=mapped(subject.artifact,(h,w),True)
            guard=mapped(protected.artifact,(h,w),True)
            invalid_values=mapped(depth.invalid_mask,(h,w),True) if depth.invalid_mask else None
            if source.alpha is not None:
                path=stack.enter_context(artifacts.input(source.alpha,suffix='.npy')).path
                stack.enter_context(read_alpha_array(path,(h,w),ctx.check_active))
            invalid=stack.enter_context(_invalid_plane(invalid_values,(h,w),artifacts.scratch,ctx.check_active))
            with prepare_relight(params,rgb,values,invalid,subject_pixels,guard,source.color,
                    artifacts.scratch,profile,depth_sigma=calibration.depth_sigma,
                    check_active=ctx.check_active) as files:
                authorize()
                staged=stage_relight_result(artifacts,params,source,files)
        ctx.check_active()
        return OperationResult(outputs=[staged.document],review_required=staged.review_required,
                               bytes_written=staged.bytes_written)
    except ValueError as exc:
        code=str(exc)
        raise OperationFailure(code if code in {'DEPTH_UNAVAILABLE','RELIGHT_COLOR_UNSUPPORTED'} else 'INPUT_INVALID') from exc


def relight_operations(config, admission_factory=None, *, runtime_lock=None, expected_runtime_sha=None):
    """No default installation until concrete live authority is configured."""
    if admission_factory is None:return []
    if not callable(admission_factory):raise ValueError('RELIGHT_ADMISSION_CONFIG_INVALID')
    from .relight_runtime_lock import verify_installed_runtime
    verify_installed_runtime(runtime_lock,expected_runtime_sha)
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.executor import Operation
    roles=frozenset({'document','working_image','pixels','mask','mask_pixels','report'})
    def run(ctx):
        with TaskArtifacts.create(ctx,config,roles) as artifacts:
            from .relight_context import verified_relight_context
            local_ctx=verified_relight_context(ctx,artifacts)
            delegate=admission_factory(local_ctx,artifacts)
            if not callable(getattr(delegate,'admit',None)):
                raise OperationFailure('RELIGHT_INPUT_AUTHORITY_UNAVAILABLE')
            class RuntimeAdmission:
                def admit(self,*args):
                    ctx.check_active()
                    if ctx.lease.execution_profile.get('relight_runtime_sha')!=expected_runtime_sha:
                        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
                    verify_installed_runtime(runtime_lock,expected_runtime_sha)
                    delegate.admit(*args)
                    ctx.check_active()
                    verify_installed_runtime(runtime_lock,expected_runtime_sha)
            return execute(local_ctx,artifacts,RuntimeAdmission())
    return [Operation('image.relight','cpu',run)]
