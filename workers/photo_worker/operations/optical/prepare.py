"""Attempt-owned lens outputs, ready for fenced artifact staging."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
from pathlib import Path
from collections.abc import Callable, Iterator
import numpy as np
from photo_worker.algorithms.lens import lens_map_tiles, resample_lens_tiles
from photo_worker.runtime.scratch import ScratchSpace, Reservation
from .contracts import LensProfile


@dataclass(frozen=True)
class LensFiles:
    rgb: Path
    alpha: Path
    inverse_field: Path
    changed_mask: Path
    reservation: Reservation
    uncovered: bool


@contextmanager
def prepare_lens(profile: LensProfile, rgb: np.ndarray, alpha: np.ndarray,
                 strength: float, scratch: ScratchSpace, *, tile: int=512,
                 check_active: Callable[[],None]=lambda:None) -> Iterator[LensFiles]:
    """Keep mappings closed before staging; remove every file on any failure.

    Output geometry is the green inverse field. Channel-specific CA affects
    pixels and coverage only. No transform artifact is committed here.
    """
    if not isinstance(alpha,np.ndarray) or alpha.ndim!=2:
        raise ValueError('LENS_IMAGE_INVALID')
    # Validate the public inputs before creating scratch output files.
    pixels=resample_lens_tiles(profile,rgb,alpha,strength,tile=tile,check_active=check_active)
    first=next(pixels)
    height,width=alpha.shape
    with scratch.reserve(height*width*28+16384) as reservation:
        rgb_path=reservation.path('lens-rgb','.npy')
        alpha_path=reservation.path('lens-alpha','.npy')
        field_path=reservation.path('lens-field','.npy')
        changed_path=reservation.path('lens-changed','.npy')
        uncovered=False
        with ExitStack() as mappings:
            def allocate(path,shape):
                mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=shape)
                mappings.callback(mapped._mmap.close)
                return mapped
            colors=allocate(rgb_path,(height,width,3))
            opacity=allocate(alpha_path,(height,width))
            field=allocate(field_path,(height,width,2))
            changed=allocate(changed_path,(height,width))
            def write(block):
                nonlocal uncovered
                h,w=block.alpha.shape
                colors[block.y:block.y+h,block.x:block.x+w]=block.rgb
                opacity[block.y:block.y+h,block.x:block.x+w]=block.alpha
                original_rgb=rgb[block.y:block.y+h,block.x:block.x+w]
                original_alpha=alpha[block.y:block.y+h,block.x:block.x+w]
                changed[block.y:block.y+h,block.x:block.x+w]=(
                    np.any(original_rgb.view(np.uint32)!=block.rgb.view(np.uint32),axis=-1) |
                    (original_alpha.view(np.uint32)!=block.alpha.view(np.uint32))).astype(np.float32)
                uncovered=uncovered or block.uncovered
            write(first)
            for block in pixels:write(block)
            for block in lens_map_tiles(profile,(height,width),strength,tile=tile,check_active=check_active):
                h,w=block.gain.shape
                field[block.y:block.y+h,block.x:block.x+w]=block.green
            colors.flush();opacity.flush();field.flush();changed.flush()
            reservation.validate()
        check_active()
        yield LensFiles(rgb_path,alpha_path,field_path,changed_path,reservation,uncovered)


@contextmanager
def prepare_working_lens(profile: LensProfile, rgb: np.ndarray, alpha: np.ndarray,
                         color, strength: float, scratch: ScratchSpace, *, tile: int=512,
                         check_active: Callable[[],None]=lambda:None) -> Iterator[LensFiles]:
    """Correct in source-primary linear RGB and publish source-encoded arrays.

    Gamma input is converted in tiles into a reserved disk-backed array. The
    prepared RGB is re-encoded in place and changed-mask recomputed against the
    original encoded bits. Exact zero strength skips both transfer functions.
    """
    from photo_worker.algorithms.warp import validate_shape
    from .color import to_working_linear, from_working_linear
    if (not isinstance(rgb,np.ndarray) or not isinstance(alpha,np.ndarray) or
            rgb.ndim!=3 or rgb.shape[-1]!=3 or rgb.dtype!=np.float32 or
            alpha.shape!=rgb.shape[:2] or alpha.dtype!=np.float32):
        raise ValueError('LENS_IMAGE_INVALID')
    validate_shape(alpha.shape)
    if type(tile) is not int or not 1<=tile<=2048:
        raise ValueError('LENS_PARAMS_INVALID')
    if type(strength) not in (int,float) or not 0<=strength<=1:
        raise ValueError('LENS_PARAMS_INVALID')
    if color.encoding=='linear' or strength==0:
        with prepare_lens(profile,rgb,alpha,strength,scratch,tile=tile,check_active=check_active) as files:
            yield files
        return
    height,width=alpha.shape
    with scratch.reserve(height*width*12+4096) as temporary:
        path=temporary.path('lens-linear-input','.npy')
        with ExitStack() as mappings:
            linear=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=rgb.shape)
            mappings.callback(linear._mmap.close)
            for top in range(0,height,tile):
                for left in range(0,width,tile):
                    check_active()
                    linear[top:top+tile,left:left+tile]=to_working_linear(rgb[top:top+tile,left:left+tile],color)
            linear.flush();temporary.validate()
            with prepare_lens(profile,linear,alpha,strength,scratch,tile=tile,check_active=check_active) as files:
                with ExitStack() as outputs:
                    def mutable(path):
                        array=np.load(path,mmap_mode='r+',allow_pickle=False)
                        outputs.callback(array._mmap.close)
                        return array
                    encoded=mutable(files.rgb);opacity=mutable(files.alpha);changed=mutable(files.changed_mask)
                    for top in range(0,height,tile):
                        for left in range(0,width,tile):
                            check_active()
                            target=encoded[top:top+tile,left:left+tile]
                            target[:]=from_working_linear(target,color)
                            original=rgb[top:top+tile,left:left+tile]
                            changed[top:top+tile,left:left+tile]=(
                                np.any(original.view(np.uint32)!=target.view(np.uint32),axis=-1)|
                                (alpha[top:top+tile,left:left+tile].view(np.uint32)!=
                                 opacity[top:top+tile,left:left+tile].view(np.uint32))).astype(np.float32)
                    encoded.flush();changed.flush();files.reservation.validate()
                check_active()
                yield files


@contextmanager
def prepare_source_lens(source, profile: LensProfile, rgb: np.ndarray, alpha,
                        strength: float, scratch: ScratchSpace, *, tile: int=512,
                        check_active: Callable[[],None]=lambda:None) -> Iterator[LensFiles]:
    """Combine source canvas/alpha binding, opacity normalization and lens math."""
    from .alpha import normalized_alpha
    shape=(source.canvas.height,source.canvas.width)
    if (source.dtype!='float32' or not isinstance(rgb,np.ndarray) or
            rgb.shape!=(*shape,3) or rgb.dtype!=np.float32 or
            (source.alpha is None)!=(alpha is None)):
        raise ValueError('LENS_SOURCE_INVALID')
    with normalized_alpha(alpha,shape,scratch,tile=tile,check_active=check_active) as opacity:
        with prepare_working_lens(profile,rgb,opacity,source.color,strength,scratch,
                                  tile=tile,check_active=check_active) as files:
            yield files
