"""Owned, closed final array files for fenced artifact staging."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from pathlib import Path
import numpy as np


@dataclass(frozen=True)
class RenderOutputFiles:
    rgb:Path|None
    alpha:Path|None
    generated:Path|None
    reservation:object
    changes:object


@contextmanager
def prepare_render_output_files(prepared,scratch,*,tile_size=512,check_active=lambda:None):
    """Copy scoped final arrays without staging borrowed intermediate files.

    Caller retains prepared lifetime. Changed-mask keeps its separate owner.
    Null RGB/alpha paths mean reuse original refs, except explicit alpha
    representation conversion always requires a new float32 file.
    """
    check_active();pixels=prepared.composition.pixels;changes=prepared.composition.changes
    if type(tile_size) is not int or tile_size not in (256,512,1024):raise ValueError('RENDER_OUTPUT_TILE_INVALID')
    arrays={'rgb':pixels.rgb if changes.rgb_changed else None,
            'alpha':pixels.alpha if changes.alpha_changed or prepared.alpha_requires_new_reference else None,
            'generated':pixels.generated}
    if (changes.alpha_changed or prepared.alpha_requires_new_reference) and pixels.alpha is None:
        raise ValueError('RENDER_OUTPUT_ALPHA_MISSING')
    required=sum(array.nbytes+8192 for array in arrays.values() if array is not None)
    with scratch.reserve(max(1,required)) as reservation:
        paths={name:None for name in arrays}
        with ExitStack() as mappings:
            for name,array in arrays.items():
                if array is None:continue
                path=reservation.path('render-final-'+name,'.npy');paths[name]=path
                mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=array.shape)
                mappings.callback(mapped._mmap.close)
                h,w=array.shape[:2]
                for y in range(0,h,tile_size):
                    for x in range(0,w,tile_size):
                        check_active();part=np.s_[y:y+tile_size,x:x+tile_size]
                        mapped[part]=array[part];reservation.validate()
                mapped.flush()
        for path in paths.values():
            if path is not None:reservation.validate_file(path)
        check_active()
        yield RenderOutputFiles(paths['rgb'],paths['alpha'],paths['generated'],reservation,changes)
        check_active()
