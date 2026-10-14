"""Scoped native Real-ESRGAN pixels; staging and geometry remain separate."""
from contextlib import contextmanager
from dataclasses import dataclass
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import resize_work_linear,transfer_model_delta,work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.restoration import UpscaleParams


@dataclass(frozen=True)
class PreparedUpscale:
    rgb_path: object
    alpha_path: object
    reservation: object
    source_clip: int
    gamut_excess: int
    disabled: int
    baseline_path: object = None


def infer(engine,value,scale):
    result=np.asarray(engine.infer(value),np.float32)
    if result.shape!=(value.shape[0]*scale,value.shape[1]*scale,3) or not np.isfinite(result).all() or np.any(result<0) or np.any(result>1):
        raise ValueError('MODEL_OUTPUT_INVALID')
    return result


@contextmanager
def prepare_upscale(source,alpha,params,engine,scratch,*,check_active=lambda:None,progress=lambda done,total:None,read_protected=None,retain_baseline=False):
    params=UpscaleParams.model_validate_json(canonical_bytes(params))
    if type(retain_baseline) is not bool:raise ValueError('UPSCALE_BASELINE_INVALID')
    if read_protected is not None and not callable(read_protected):raise ValueError('UPSCALE_PROTECTION_INVALID')
    if (not isinstance(source,np.ndarray) or source.dtype!=np.float32 or source.ndim!=3
            or source.shape[2]!=3 or min(source.shape[:2])<1
            or source.shape[0]*source.shape[1]*params.scale**2>200000000):
        raise ValueError('UPSCALE_IMAGE_INVALID')
    height,width=source.shape[:2];oh,ow=height*params.scale,width*params.scale
    if alpha is not None and (not isinstance(alpha,np.ndarray) or alpha.shape!=(height,width)
            or alpha.dtype not in (np.dtype('uint16'),np.dtype('float32'))):raise ValueError('UPSCALE_ALPHA_INVALID')
    for y in range(0,height,256):
        check_active();tile=source[y:y+256]
        if not np.isfinite(tile).all() or np.any(tile<0) or np.any(tile>1):raise ValueError('UPSCALE_IMAGE_INVALID')
        if alpha is not None and alpha.dtype==np.float32:
            tile=alpha[y:y+256]
            if not np.isfinite(tile).all() or np.any(tile<0) or np.any(tile>1):raise ValueError('UPSCALE_ALPHA_INVALID')
    source_clip=gamut_excess=disabled=done=0
    total=((height+255)//256)*((width+255)//256)+3
    reserve_bytes = oh * ow * (12 + (12 if retain_baseline else 0) + (alpha.dtype.itemsize if alpha is not None else 0)) + 32768
    with scratch.reserve(reserve_bytes) as reservation:
        rgb_path = reservation.path("upscaled-rgb", ".npy")
        output = np.lib.format.open_memmap(rgb_path, mode="w+", dtype="<f4", shape=(oh, ow, 3))
        alpha_path = reservation.path("upscaled-alpha", ".npy") if alpha is not None else None
        out_alpha=None;baseline=None
        baseline_path=reservation.path('upscale-baseline','.npy') if retain_baseline else None
        try:
            if baseline_path is not None:baseline=np.lib.format.open_memmap(baseline_path,mode='w+',dtype='<f4',shape=(oh,ow,3))
            if alpha_path is not None:out_alpha=np.lib.format.open_memmap(alpha_path,mode="w+",dtype=alpha.dtype,shape=(oh,ow))
            for y in range(0, height, 256):
                for x in range(0, width, 256):
                    check_active(); y1 = min(y + 256, height); x1 = min(x + 256, width)
                    hy0 = max(0, y - 32); hx0 = max(0, x - 32); hy1 = min(height, y1 + 32); hx1 = min(width, x1 + 32)
                    work = np.asarray(source[hy0:hy1, hx0:hx1], np.float32); model_in, _ = work_to_model(work)
                    model_out = infer(engine, model_in, params.scale)
                    base_work = resize_work_linear(work, (work.shape[1] * params.scale, work.shape[0] * params.scale))
                    model_base = cv2.resize(model_in, (model_out.shape[1], model_out.shape[0]), interpolation=cv2.INTER_CUBIC)
                    model_base = np.clip(model_base, 0, 1).astype(np.float32)
                    restored, evidence = transfer_model_delta(base_work, model_base, model_out, 1.)
                    cy0 = (y - hy0) * params.scale; cx0 = (x - hx0) * params.scale
                    cy1 = cy0 + (y1 - y) * params.scale; cx1 = cx0 + (x1 - x) * params.scale
                    core = (slice(cy0, cy1), slice(cx0, cx1)); oy = slice(y * params.scale, y1 * params.scale); ox = slice(x * params.scale, x1 * params.scale)
                    if baseline is not None:baseline[oy,ox]=base_work[core]
                    target_core=restored[core].copy()
                    if read_protected is not None:
                        check_active()
                        protected=read_protected(x*params.scale,y*params.scale,(x1-x)*params.scale,(y1-y)*params.scale)
                        if (not isinstance(protected,np.ndarray) or protected.dtype!=np.float32
                                or protected.shape!=target_core.shape[:2] or not np.isfinite(protected).all()
                                or np.any(protected<0) or np.any(protected>1)):
                            raise ValueError('UPSCALE_PROTECTION_INVALID')
                        selected=protected>0
                        target_core[selected]=base_work[core][selected]
                    output[oy, ox] = target_core
                    source_clip += int(np.count_nonzero(evidence.source_clipped[core])); gamut_excess += int(np.count_nonzero(evidence.gamut_excess[core])); disabled += int(np.count_nonzero(evidence.disabled[core]))
                    done += 1; progress(done, total)
            if out_alpha is not None:
                out_alpha[:] = cv2.resize(np.asarray(alpha), (ow, oh), interpolation=cv2.INTER_LINEAR).astype(alpha.dtype)
                out_alpha.flush()
            if baseline is not None:baseline.flush()
            output.flush(); reservation.validate()
        finally:
            output._mmap.close()
            if out_alpha is not None: out_alpha._mmap.close()
            if baseline is not None:baseline._mmap.close()
        check_active()
        yield PreparedUpscale(rgb_path,alpha_path,reservation,source_clip,gamut_excess,disabled,baseline_path)
        check_active()


def upscale_geometry(image,params):
    """Exact existing source identity and half-pixel resize, without publication."""
    from photo_worker.contracts.images import WorkingImage,Canvas,Rect,ResizeTransform,TransformChain
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    params=UpscaleParams.model_validate_json(canonical_bytes(params))
    width=image.canvas.width*params.scale;height=image.canvas.height*params.scale
    if width*height>200000000:raise ValueError('UPSCALE_PIXEL_LIMIT')
    canvas=Canvas(width=width,height=height,orientation=1,
        space_id=f'up{params.scale}-{image.canvas.source_sha256[:16]}',source_sha256=image.canvas.source_sha256)
    step=ResizeTransform(kind='resize',from_canvas=image.canvas,to_canvas=canvas,invertibility='invertible',
        valid_region=Rect(x=0,y=0,width=width,height=height),pixel_centers='half_pixel')
    return canvas,TransformChain.create([*image.transform_chain.steps,step])
