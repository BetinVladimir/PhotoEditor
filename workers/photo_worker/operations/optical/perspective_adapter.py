"""Perspective worker pipeline; qualified admission is required before pixel IO."""
from contextlib import ExitStack
from typing import Protocol
from photo_worker.contracts.base import canonical_hash,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.optical_perspective import OpticalPerspectiveParams,LineSegments
from photo_worker.runtime.arrays import read_array,read_alpha_array
from photo_worker.runtime.executor import OperationFailure,OperationResult
from .contracts import ImageReport,ImageResult
from .perspective_geometry import resolve_perspective_geometry
from .perspective_resources import admit_perspective
from .perspective_prepare import prepare_source_perspective
from .perspective_result import stage_perspective_result


class PerspectiveAdmission(Protocol):
    def admit(self,ctx,params,source,lines)->int:
        """Verify pinned runtime/qualification, source/line authority and ICC.

        Return operator-approved max output pixels (1..200M). Must not derive
        qualification from user intent or return an invented success report.
        """
        ...


def _zero_result(ctx,artifacts,params,source,shape):
    """Publish only zero mask and report; preserve exact source image reference."""
    import numpy as np
    height,width=shape
    with artifacts.scratch.reserve(height*width*4+4096) as reservation:
        path=reservation.path('perspective-zero','.npy')
        mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=shape)
        try:
            for top in range(0,height,512):ctx.check_active();mapped[top:top+512]=0
            mapped.flush();reservation.validate()
        finally:mapped._mmap.close()
        mask_pixels=artifacts.stage(path,reservation,'mask_pixels','application/x-npy')
        written=path.stat().st_size
    mask=MaskRef(artifact=mask_pixels,canvas=source.canvas,transform_chain=source.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    mask_ref=artifacts.stage_contract(mask,'mask');written+=len(canonical_bytes(mask))
    report=ImageReport(schema_version=1,status='pass',operation='optical.perspective',skipped_count=0,
        reason=None,outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,sources=[params.base],registrations=[])
    report_ref=artifacts.stage_contract(report,'report');written+=len(canonical_bytes(report))
    result=ImageResult(schema_version=1,working_image=params.base.source,report=report_ref,changed_mask=mask_ref,generated_mask=None)
    document=artifacts.stage_contract(result,'document');written+=len(canonical_bytes(result))
    return OperationResult(outputs=[document],review_required=False,bytes_written=written)


def execute(ctx,artifacts,admission:PerspectiveAdmission|None):
    profile=ctx.lease.execution_profile
    if profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2':raise OperationFailure('CONFIG_INVALID')
    if admission is None:raise OperationFailure('PERSPECTIVE_QUALIFICATION_UNAVAILABLE')
    try:
        params=artifacts.params(OpticalPerspectiveParams)
        # Own mutable contracts across admission callbacks and IO.
        params=OpticalPerspectiveParams.model_validate_json(canonical_bytes(params))
        source=artifacts.contract(params.base.source,WorkingImage)
        source=WorkingImage.model_validate_json(canonical_bytes(source))
        if source.dtype!='float32' or canonical_hash(source.canvas)!=params.base.canvas_hash:
            raise ValueError('PERSPECTIVE_SOURCE_MISMATCH')
        lines=None if params.lines is None else artifacts.contract(params.lines,LineSegments)
        if lines is not None:lines=LineSegments.model_validate_json(canonical_bytes(lines))
        from .perspective_source import verify_perspective_source_closure
        verify_perspective_source_closure(artifacts,params,source,lines)
        ctx.check_active()
        max_pixels=admission.admit(ctx,params.model_copy(deep=True),source.model_copy(deep=True),None if lines is None else lines.model_copy(deep=True))
        if type(max_pixels) is not int or not 1<=max_pixels<=200000000:raise OperationFailure('CONFIG_INVALID')
        geometry=resolve_perspective_geometry(params,source,lines,max_pixels=max_pixels,check_active=ctx.check_active)
        admit_perspective(profile,source.canvas.width,source.canvas.height,geometry.width,geometry.height)
        ctx.check_active()
        if params.strength==0:return _zero_result(ctx,artifacts,params,source,(source.canvas.height,source.canvas.width))
        ctx.progress(0,3)
        with ExitStack() as inputs:
            path=inputs.enter_context(artifacts.input(source.pixels,suffix='.npy')).path
            rgb=inputs.enter_context(read_array(path,(source.canvas.height,source.canvas.width,3),'float32',ctx.check_active))
            alpha=None
            if source.alpha is not None:
                path=inputs.enter_context(artifacts.input(source.alpha,suffix='.npy')).path
                alpha=inputs.enter_context(read_alpha_array(path,(source.canvas.height,source.canvas.width),ctx.check_active))
            with prepare_source_perspective(source,rgb,alpha,geometry,artifacts.scratch,check_active=ctx.check_active) as files:
                ctx.progress(1,3)
                result=stage_perspective_result(artifacts,params.base.source,source,params.base,files,mode=params.mode)
        ctx.progress(3,3)
        return OperationResult(outputs=[result.document],review_required=result.review_required,bytes_written=result.bytes_written)
    except ValueError as error:
        code=str(error)
        allowed={'PERSPECTIVE_SOURCE_MISMATCH','PERSPECTIVE_CROP_LIMIT','PERSPECTIVE_PIXEL_LIMIT',
            'PERSPECTIVE_DEGENERATE','PERSPECTIVE_HORIZON','PERSPECTIVE_HORIZON_UNSUPPORTED',
            'PERSPECTIVE_LINE_FAMILY_DEGENERATE','OPTICAL_COLOR_OVERFLOW','OPTICAL_COLOR_UNSUPPORTED'}
        raise OperationFailure(code if code in allowed else 'INPUT_INVALID') from error
