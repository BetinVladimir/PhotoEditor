"""Native geometry replay primitives using adopted, independently verified plans.

No routine here selects evidence, approves a policy, or fabricates task outputs.
The complete Region installer must bind source/Params/report and account for
full-frame native temporaries before entering these contexts.
"""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
import numpy as np
from photo_worker.algorithms.body_warp import warp_rgb_into, warp_alpha_into
from photo_worker.runtime.arrays import read_array
from photo_worker.operations.geometry.prepare import prepare_crop_stage


@dataclass(frozen=True)
class GeometryPixels:
    rgb: object
    alpha: object
    canvas: object
    transform_chain: object


def _alpha16(alpha):
    if alpha is None:return None
    if alpha.dtype==np.uint16:return alpha
    if alpha.dtype!=np.float32 or not np.isfinite(alpha).all() or np.any((alpha<0)|(alpha>1)):
        raise ValueError('REGION_GEOMETRY_ALPHA_INVALID')
    # Region source loading already proves the physical source is uint16. This
    # reverses that exact normalized representation before native alpha warp.
    result=np.rint(alpha*65535).astype(np.uint16)
    if not np.array_equal(result.astype(np.float32)/65535,alpha):
        raise ValueError('REGION_GEOMETRY_ALPHA_NOT_UINT16')
    return result


@contextmanager
def prepare_region_crop(image_ref,image,plan_ref,plan,rgb,alpha,scratch,*,check_active=lambda:None):
    with prepare_crop_stage(image_ref,image,plan_ref,plan,rgb,_alpha16(alpha),scratch,
            check_active=check_active) as result:
        yield GeometryPixels(result.rgb,None if result.alpha is None else result.alpha.astype(np.float32)/65535,
            result.canvas,result.transform_chain)
        check_active()


@contextmanager
def prepare_region_body(image,target,report,rgb,alpha,inverse_field,scratch,*,check_active=lambda:None):
    """Reuse exact approved inverse field and original skip/applied semantics."""
    h,w=image.canvas.height,image.canvas.width
    if (h*w>40000000 or rgb.dtype!=np.float32 or rgb.shape!=(h,w,3)
            or target.canvas.width!=w or target.canvas.height!=h
            or inverse_field.dtype!=np.float32 or inverse_field.shape!=(h,w,2)):
        raise ValueError('REGION_BODY_GEOMETRY_INVALID')
    for y in range(0,h,256):
        for x in range(0,w,256):
            check_active()
            if not np.isfinite(inverse_field[y:y+256,x:x+256]).all():raise ValueError('REGION_BODY_FIELD_INVALID')
    if not report.applied:
        # Native Apply safety skips preserve RGB bits instead of round-tripping
        # linear/encoded values through an identity sampler.
        yield GeometryPixels(rgb,alpha,target.canvas,target.transform_chain)
        check_active();return
    original_alpha=_alpha16(alpha)
    with scratch.reserve(h*w*(12+(2 if alpha is not None else 0))+65536) as owner:
        path=owner.path('region-body','.npy')
        output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w,3))
        try:
            check_active();warp_rgb_into(rgb,original_alpha,inverse_field,output,check_active=check_active)
            output.flush();check_active()
        finally:output._mmap.close()
        alpha_path=None
        if original_alpha is not None:
            alpha_path=owner.path('region-body-alpha','.npy')
            a=np.lib.format.open_memmap(alpha_path,mode='w+',dtype='<u2',shape=(h,w))
            try:
                check_active();warp_alpha_into(original_alpha,inverse_field,a,check_active=check_active)
                a.flush()
            finally:a._mmap.close()
        owner.validate()
        with ExitStack() as retained:
            result=retained.enter_context(read_array(path,(h,w,3),'float32',check_active,unit_interval=True))
            result_alpha=None if alpha_path is None else retained.enter_context(read_array(alpha_path,(h,w),'uint16',check_active))
            yield GeometryPixels(result,None if result_alpha is None else result_alpha.astype(np.float32)/65535,target.canvas,target.transform_chain)
            check_active()


@contextmanager
def prepare_region_upscale(params,target,rgb,alpha,provider,scratch,*,backend,model_lock_sha,
                           check_active=lambda:None):
    from photo_worker.operations.image.prepare_upscale import prepare_upscale
    h,w=rgb.shape[:2];oh,ow=h*params.scale,w*params.scale
    if (provider is None or provider.backend!=backend or provider.pool.lock.lock_id!=model_lock_sha
            or params.model_lock_id!=model_lock_sha or target.canvas.width!=ow or target.canvas.height!=oh):
        raise ValueError('REGION_UPSCALE_RUNTIME_UNAVAILABLE')
    provider.verify_runtime();check_active()
    with provider.pool.acquire(params.model_id,backend) as engine:
        with prepare_upscale(rgb,_alpha16(alpha),params,engine,scratch,check_active=check_active) as result:
            with ExitStack() as retained:
                pixels=retained.enter_context(read_array(result.rgb_path,(oh,ow,3),'float32',check_active,unit_interval=True))
                result_alpha=None if result.alpha_path is None else retained.enter_context(read_array(result.alpha_path,(oh,ow),'uint16',check_active))
                yield GeometryPixels(pixels,None if result_alpha is None else result_alpha.astype(np.float32)/65535,
                    target.canvas,target.transform_chain)
                check_active();provider.verify_runtime()
