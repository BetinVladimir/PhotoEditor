"""Attempt-owned perspective files; no artifact publication or admission."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.perspective import PerspectiveGeometry,checked_homography,warp_perspective_tiles
from photo_worker.runtime.scratch import Reservation
from .alpha import normalized_alpha
from .color import to_working_linear,from_working_linear


@dataclass(frozen=True)
class PerspectiveFiles:
    rgb:Path
    alpha:Path
    changed_mask:Path
    reservation:Reservation
    geometry:PerspectiveGeometry
    uncovered:bool
    clipping_fraction:float


@contextmanager
def prepare_source_perspective(source,rgb,alpha,geometry:PerspectiveGeometry,scratch,*,tile=512,check_active=lambda:None):
    """Convert transfer in bounded tiles, warp linearly, restore source encoding.

    Nonidentity geometry marks every output pixel changed, including uncovered
    pixels. No content is generated. Identity preserves encoded RGB bits under
    nonzero alpha and zeros colors under fully transparent pixels.
    """
    shape=(source.canvas.height,source.canvas.width)
    if (source.dtype!='float32' or not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or
        rgb.shape!=(*shape,3) or (source.alpha is None)!=(alpha is None)):
        raise ValueError('PERSPECTIVE_SOURCE_INVALID')
    if type(tile) is not int or not 1<=tile<=2048:raise ValueError('PERSPECTIVE_TILE_INVALID')
    h,w=geometry.height,geometry.width
    if any(type(v) is not int or v<1 for v in (h,w)) or h*w>200000000:
        raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
    matrix=checked_homography(geometry.source_to_output,[(0,0),(shape[1],0),(shape[1],shape[0]),(0,shape[0])])
    # Own the plan across yields: a mutable array supplied by a caller cannot
    # change pixels midway through one attempt.
    geometry=PerspectiveGeometry(matrix,w,h,geometry.crop_fraction)
    identity=shape==(h,w) and np.array_equal(matrix,np.eye(3))
    with normalized_alpha(alpha,shape,scratch,tile=tile,check_active=check_active) as opacity:
        with ExitStack() as lifetime:
            linear=rgb
            if source.color.encoding!='linear' and not identity:
                temporary=lifetime.enter_context(scratch.reserve(shape[0]*shape[1]*12+4096))
                linear=np.lib.format.open_memmap(temporary.path('perspective-linear','.npy'),mode='w+',dtype='<f4',shape=rgb.shape)
                lifetime.callback(linear._mmap.close)
                for top in range(0,shape[0],tile):
                    for left in range(0,shape[1],tile):
                        check_active();linear[top:top+tile,left:left+tile]=to_working_linear(rgb[top:top+tile,left:left+tile],source.color)
                linear.flush();temporary.validate()
            with scratch.reserve(h*w*20+16384) as reservation:
                rgb_path=reservation.path('perspective-rgb','.npy')
                alpha_path=reservation.path('perspective-alpha','.npy')
                changed_path=reservation.path('perspective-changed','.npy')
                uncovered=False;clipped=0
                with ExitStack() as mappings:
                    def allocate(path,dimensions):
                        mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=dimensions)
                        mappings.callback(mapped._mmap.close)
                        return mapped
                    colors=allocate(rgb_path,(h,w,3));out_alpha=allocate(alpha_path,(h,w));changed=allocate(changed_path,(h,w))
                    if identity:
                        # Bypass transfer and interpolation to avoid unnecessary
                        # gamma round-trip drift in a true identity correction.
                        for top in range(0,h,tile):
                            for left in range(0,w,tile):
                                check_active();original=rgb[top:top+tile,left:left+tile];a=opacity[top:top+tile,left:left+tile]
                                if not np.isfinite(original).all():raise ValueError('PERSPECTIVE_IMAGE_INVALID')
                                block=np.where(a[...,None]>0,original,0).astype(np.float32)
                                colors[top:top+tile,left:left+tile]=block;out_alpha[top:top+tile,left:left+tile]=a
                                changed[top:top+tile,left:left+tile]=np.any(block.view(np.uint32)!=original.view(np.uint32),axis=-1)
                                linear_block=to_working_linear(block,source.color)
                                clipped+=int(np.count_nonzero(np.any((linear_block>=1)|(linear_block<0),axis=-1)))
                                uncovered=uncovered or bool(np.any(a==0))
                    else:
                        for block in warp_perspective_tiles(linear,opacity,geometry,tile=tile,check_active=check_active):
                            bh,bw=block.alpha.shape;target=(slice(block.y,block.y+bh),slice(block.x,block.x+bw))
                            colors[target]=from_working_linear(block.rgb,source.color)
                            out_alpha[target]=block.alpha;changed[target]=1.
                            clipped+=int(np.count_nonzero(np.any((block.rgb>=1)|(block.rgb<0),axis=-1)))
                            uncovered=uncovered or bool(np.any(block.alpha==0))
                    colors.flush();out_alpha.flush();changed.flush();reservation.validate()
                check_active()
                yield PerspectiveFiles(rgb_path,alpha_path,changed_path,reservation,geometry,uncovered,clipped/(h*w))
