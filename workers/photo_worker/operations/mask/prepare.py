from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from pathlib import Path
from collections.abc import Callable,Iterator
import numpy as np
from photo_worker.contracts.manual_mask import ManualMask
from photo_worker.algorithms.manual_mask import rasterize
from photo_worker.algorithms.mask_png import write_png16,png16_bound
from photo_worker.runtime.scratch import ScratchSpace,Reservation

@dataclass(frozen=True)
class RasterFiles:
    target:Path
    protected:Path
    target_png:Path
    protected_png:Path
    arrays:Reservation
    pngs:Reservation
    protected_nonzero:bool


@contextmanager
def prepare_raster(mask:ManualMask,source_sha256:str,scratch:ScratchSpace,*,tile:int=512,
                   check_active:Callable[[],None]=lambda:None)->Iterator[RasterFiles]:
    """Borrow output paths until context exit; all memory maps close before yield."""
    mask.coherent();check_active()
    if mask.source_sha256!=source_sha256:raise ValueError('OVERRIDE_SOURCE_MISMATCH')
    h,w=mask.canvas.height,mask.canvas.width
    with scratch.reserve(h*w*8+8192) as arrays, scratch.reserve(2*png16_bound(w,h)) as pngs:
        target_path=arrays.path('mask-target','.npy');protected_path=arrays.path('mask-protected','.npy')
        target_png=pngs.path('mask-target','.png');protected_png=pngs.path('mask-protected','.png')
        with ExitStack() as mappings:
            protected_nonzero=False
            target=np.lib.format.open_memmap(target_path,mode='w+',dtype='<f4',shape=(h,w))
            mappings.callback(target._mmap.close)
            protected=np.lib.format.open_memmap(protected_path,mode='w+',dtype='<f4',shape=(h,w))
            mappings.callback(protected._mmap.close)
            for part in rasterize(mask,source_sha256,tile_size=tile,check_active=check_active):
                th,tw=part.target.shape;sl=(slice(part.y,part.y+th),slice(part.x,part.x+tw))
                target[sl]=part.target;protected[sl]=part.protected
                if not protected_nonzero and np.any(part.protected>0):protected_nonzero=True
            target.flush();protected.flush();arrays.validate()
            for source,path in ((target,target_png),(protected,protected_png)):
                with scratch.open_writer(path,pngs) as stream:
                    write_png16(source,stream,max_bytes=png16_bound(w,h),check_active=check_active)
            pngs.validate()
        check_active()
        yield RasterFiles(target_path,protected_path,target_png,protected_png,arrays,pngs,protected_nonzero)
