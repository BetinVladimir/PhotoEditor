from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from pathlib import Path
from collections.abc import Callable,Iterator
import numpy as np
from photo_worker.contracts.images import TransformStep
from photo_worker.algorithms.mask_transform import transform_tiles
from photo_worker.algorithms.mask_refine import refine_tiles
from photo_worker.algorithms.mask_png import write_png16,png16_bound
from photo_worker.runtime.scratch import ScratchSpace,Reservation

@dataclass(frozen=True)
class TransformedFiles:
    array:Path
    png:Path
    arrays:Reservation
    pngs:Reservation
    radius:int|None


@contextmanager
def prepare_transform(source:np.ndarray,step:TransformStep,scratch:ScratchSpace,*,guide:np.ndarray|None=None,
                      labels:bool=False,inverse_field:np.ndarray|None=None,tile:int=512,max_work_pixels:int=4194304,
                      check_active:Callable[[],None]=lambda:None)->Iterator[TransformedFiles]:
    check_active()
    if (guide is None)!=labels:raise ValueError('Probability masks require target guide; labels forbid it')
    h,w=step.to_canvas.height,step.to_canvas.width
    radius=None if labels else max(1,round(8*max(h,w)/1024))
    with scratch.reserve(h*w*(2 if labels else 8)+8192) as arrays,scratch.reserve(png16_bound(w,h)) as pngs:
        raw_path=arrays.path('transformed-mask','.npy')
        final_path=raw_path if labels else arrays.path('refined-mask','.npy')
        png_path=pngs.path('transformed-mask','.png')
        with ExitStack() as mappings:
            raw=np.lib.format.open_memmap(raw_path,mode='w+',dtype='<u2' if labels else '<f4',shape=(h,w))
            mappings.callback(raw._mmap.close)
            for part in transform_tiles(source,step,labels=labels,inverse_field=inverse_field,tile_size=tile,check_active=check_active):
                th,tw=part.values.shape;raw[part.y:part.y+th,part.x:part.x+tw]=part.values
            raw.flush();arrays.validate()
            final=raw
            if not labels:
                final=np.lib.format.open_memmap(final_path,mode='w+',dtype='<f4',shape=(h,w))
                mappings.callback(final._mmap.close)
                for part in refine_tiles(raw,guide,radius=radius,tile_size=tile,max_work_pixels=max_work_pixels,
                                         valid_region=step.valid_region,check_active=check_active):
                    th,tw=part.values.shape;final[part.y:part.y+th,part.x:part.x+tw]=part.values
                final.flush();arrays.validate()
            with scratch.open_writer(png_path,pngs) as stream:
                write_png16(final,stream,max_bytes=png16_bound(w,h),check_active=check_active)
            pngs.validate()
        check_active()
        yield TransformedFiles(final_path,png_path,arrays,pngs,radius)
