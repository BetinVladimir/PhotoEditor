"""Declared immutable WorkingImage arrays with explicit legacy-alpha policy."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.runtime.arrays import read_array,read_alpha_array
from photo_worker.operations.layers.alpha import layer_alpha
from photo_worker.operations.optical.contracts import ImageResult
from .render_adjustments import AdjustmentPixels


@dataclass(frozen=True)
class RenderSourcePixels:
    pixels:AdjustmentPixels
    alpha_requires_new_reference:bool


@contextmanager
def load_render_source_pixels(artifacts,source,*,max_input_bytes:int,
                              alpha_mode='require_float32',source_result=None,source_ref=None,check_active=lambda:None):
    """TaskArtifacts.input proves declared pins and verifies actual bytes.

    uint16 RGB requires the explicit color conversion stage; no implicit RGB
    conversion occurs. Alpha normalization uses the existing bounded algorithm
    and exposes its representation change for subsequent publication.
    """
    check_active()
    if (not isinstance(source,WorkingImage) or source.dtype!='float32'
            or type(max_input_bytes) is not int or not 1<=max_input_bytes<=2**53-1
            or alpha_mode not in ('require_float32','normalize_uint16')):
        raise ValueError('RENDER_SOURCE_INPUT_INVALID')
    if source_result is not None and (not isinstance(source_result,ImageResult) or source_ref is None or source_result.working_image!=source_ref):
        raise ValueError('RENDER_SOURCE_PARENT_CHANGED')
    generated_ref=source_result.generated_mask if source_result is not None else None
    h,w=source.canvas.height,source.canvas.width
    if h*w>200000000:raise ValueError('RENDER_SOURCE_CANVAS_BUDGET')
    required=h*w*(12+(4 if source.alpha is not None else 0))+8192*(1+(source.alpha is not None))
    if alpha_mode=='normalize_uint16' and source.alpha is not None:required+=h*w*4+8192
    if generated_ref is not None:required+=h*w*4+8192
    if required>max_input_bytes:raise ValueError('RENDER_SOURCE_INPUT_BUDGET')
    mask=None
    if generated_ref is not None:
        mask=artifacts.contract(generated_ref,MaskRef);check_active()
        if mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain or mask.selector!='generated' or mask.protected:
            raise ValueError('RENDER_SOURCE_GENERATED_GEOMETRY_CHANGED')
    with ExitStack() as stack:
        local=stack.enter_context(artifacts.input(source.pixels,suffix='.npy'))
        rgb=stack.enter_context(read_array(local.path,(h,w,3),'float32',check_active))
        alpha=None
        if source.alpha is not None:
            local=stack.enter_context(artifacts.input(source.alpha,suffix='.npy'))
            alpha=stack.enter_context(read_alpha_array(local.path,(h,w),check_active) if alpha_mode=='normalize_uint16'
                else read_array(local.path,(h,w),'float32',check_active,unit_interval=True))
        normalized=stack.enter_context(layer_alpha(alpha,(h,w),artifacts.scratch,mode=alpha_mode,check_active=check_active))
        generated=None
        if mask is not None:
            local=stack.enter_context(artifacts.input(mask.artifact,suffix='.npy'))
            generated=stack.enter_context(read_array(local.path,(h,w),'float32',check_active,unit_interval=True))
            flat=generated.reshape(-1)
            for start in range(0,flat.size,262144):
                check_active();block=flat[start:start+262144]
                if not np.all((block==0)|(block==1)):raise ValueError('RENDER_SOURCE_GENERATED_NOT_BINARY')
        check_active()
        yield RenderSourcePixels(AdjustmentPixels(rgb,normalized.values,generated),normalized.requires_new_reference)
        check_active()
