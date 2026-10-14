"""CPU panorama/focus worker adapter; requires independently installed admission."""
from contextlib import ExitStack
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
from photo_worker.runtime.arrays import read_array,read_alpha_array
from photo_worker.runtime.executor import OperationFailure,OperationResult
from .multi_source_closure import verify_multi_source_closure
from .multi_admitted_prepare import prepare_admitted_multi
from .multi_result import stage_multi_result


def execute(ctx,artifacts,admission,*,operation):
    profile=ctx.lease.execution_profile
    if (operation not in ('optical.panorama','optical.focus_stack') or
            profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    if admission is None or admission.operation!=operation:
        raise OperationFailure('MULTI_QUALIFICATION_UNAVAILABLE')
    estimate=profile.get('resource_estimate')
    if not isinstance(estimate,dict):raise OperationFailure('CONFIG_INVALID')
    ram=estimate.get('ram_bytes')
    if type(ram) is not int or ram<=0:raise OperationFailure('CONFIG_INVALID')
    try:
        model=OpticalPanoramaParams if operation=='optical.panorama' else OpticalFocusStackParams
        params=model.model_validate_json(canonical_bytes(artifacts.params(model)))
        images=tuple(WorkingImage.model_validate_json(canonical_bytes(artifacts.contract(binding.source,WorkingImage)))
            for binding in params.frames)
        verify_multi_source_closure(artifacts,params,images)
        # Authorize original roots and installed byte evidence before any pixel IO.
        admission.admit(ctx,params.model_copy(deep=True),tuple(image.model_copy(deep=True) for image in images))
        ctx.progress(0,3)
        with ExitStack() as lifetime:
            rgbs=[];alphas=[]
            for image in images:
                ctx.check_active();shape=(image.canvas.height,image.canvas.width)
                path=lifetime.enter_context(artifacts.input(image.pixels,suffix='.npy')).path
                rgbs.append(lifetime.enter_context(read_array(path,(*shape,3),'float32',ctx.check_active)))
                alpha=None
                if image.alpha is not None:
                    path=lifetime.enter_context(artifacts.input(image.alpha,suffix='.npy')).path
                    alpha=lifetime.enter_context(read_alpha_array(path,shape,ctx.check_active))
                alphas.append(alpha)
            ctx.progress(1,3)
            with prepare_admitted_multi(ctx,admission,params,images,rgbs,alphas,artifacts.scratch,
                    ram_max_bytes=ram) as prepared:
                ctx.progress(2,3)
                staged=stage_multi_result(artifacts,prepared,check_active=ctx.check_active)
        ctx.progress(3,3)
        return OperationResult(outputs=[staged.document],review_required=staged.review_required,
            bytes_written=staged.bytes_written)
    except ValueError as error:
        allowed={'MULTI_SOURCE_MISMATCH','MULTI_SOURCE_CHILD_UNDECLARED','MULTI_SOURCE_CHILD_ALIAS',
            'MULTI_PIXEL_LIMIT','MULTI_CALIBRATION_MISMATCH','FOCUS_MOTION_INVALID',
            'FOCUS_RAM_EXCEEDED','OPTICAL_COLOR_OVERFLOW','OPTICAL_COLOR_UNSUPPORTED'}
        code=str(error)
        raise OperationFailure(code if code in allowed else 'INPUT_INVALID') from error
