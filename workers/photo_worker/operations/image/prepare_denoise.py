"""Scoped halo-tiled SCUNet pixels with original wide-gamut delta math."""
from contextlib import contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.algorithms.model_delta import transfer_model_delta,work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.restoration import DenoiseParams


@dataclass(frozen=True)
class PreparedDenoise:
    path: object
    reservation: object
    source_clip: int
    gamut_excess: int
    disabled: int


def infer(engine,value):
    result=np.asarray(engine.infer(value),np.float32)
    if result.shape!=value.shape or not np.isfinite(result).all() or np.any(result<0) or np.any(result>1):
        raise ValueError('MODEL_OUTPUT_INVALID')
    return result


@contextmanager
def prepare_denoise(source,params,engine,scratch,*,check_active=lambda:None,progress=lambda done,total:None):
    params=DenoiseParams.model_validate_json(canonical_bytes(params))
    if (not isinstance(source,np.ndarray) or source.dtype!=np.float32 or source.ndim!=3
            or source.shape[2]!=3 or min(source.shape[:2])<1 or source.shape[0]*source.shape[1]>200000000):
        raise ValueError('DENOISE_IMAGE_INVALID')
    height,width=source.shape[:2]
    for y in range(0,height,256):
        check_active();tile=source[y:y+256]
        if not np.all(np.isfinite(tile)) or np.any(tile<0) or np.any(tile>1):raise ValueError('DENOISE_IMAGE_INVALID')
    total=((height+params.tile-1)//params.tile)*((width+params.tile-1)//params.tile)+2
    source_clip=gamut_excess=disabled=done=0
    with scratch.reserve(height*width*12+16384) as reservation:
        output_path=reservation.path('denoised-rgb','.npy')
        output=np.lib.format.open_memmap(output_path,mode='w+',dtype='<f4',shape=(height,width,3))
        try:
            for y in range(0, height, params.tile):
                for x in range(0, width, params.tile):
                    check_active(); y1 = min(y + params.tile, height); x1 = min(x + params.tile, width)
                    hy0 = max(0, y - params.overlap); hx0 = max(0, x - params.overlap)
                    hy1 = min(height, y1 + params.overlap); hx1 = min(width, x1 + params.overlap)
                    work = np.asarray(source[hy0:hy1, hx0:hx1], np.float32)
                    model_input, _ = work_to_model(work)
                    model_output = infer(engine, model_input)
                    restored, evidence = transfer_model_delta(work, model_input, model_output, params.strength)
                    core = (slice(y - hy0, y1 - hy0), slice(x - hx0, x1 - hx0))
                    output[y:y1, x:x1] = restored[core]
                    source_clip += int(np.count_nonzero(evidence.source_clipped[core]))
                    gamut_excess += int(np.count_nonzero(evidence.gamut_excess[core]))
                    disabled += int(np.count_nonzero(evidence.disabled[core]))
                    done += 1; progress(done, total)
            output.flush();reservation.validate()
        finally:output._mmap.close()
        check_active()
        yield PreparedDenoise(output_path,reservation,source_clip,gamut_excess,disabled)
        check_active()
