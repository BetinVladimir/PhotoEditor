"""ICC-verified original WorkingImage conversion for V29 multi-source math."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage, ColorSpace
from photo_worker.algorithms.multi_registration import _shapes
from .alpha import normalized_alpha
from .color import to_working_linear


@dataclass(frozen=True)
class LinearMultiSources:
    images: tuple[WorkingImage, ...]
    rgb: tuple[np.ndarray, ...]
    alpha: tuple[np.ndarray, ...]
    color: ColorSpace
    luma: tuple[float, float, float]


def multi_source_resources(shapes, *, tile=256):
    owned=_shapes(shapes)
    if type(tile) is not int or not 1<=tile<=512:raise ValueError('MULTI_SOURCE_INVALID')
    pixels=sum(h*w for h,w in owned)
    # RGB linear copies and worst-case normalized alpha files, including headers.
    scratch=16*pixels+2*len(owned)*4096
    ram=16*pixels+scratch+128*tile*tile+256*1024*1024
    if max(ram,scratch)>2**53-1:raise ValueError('MULTI_SOURCE_INVALID')
    return dict(ram_bytes=ram,scratch_bytes=scratch,vram_bytes=0)


@contextmanager
def prepare_multi_sources(images,rgbs,alphas,scratch,*,verify_icc,ram_max_bytes,
                          tile=256,check_active=lambda:None):
    """Verify metadata/ICC and retain bounded, source-primary linear copies.

    Exact object SHA/version, native float32 NPY headers and tenant source scope
    are adapter obligations before this helper. Mixed ICC identities are refused
    rather than silently applying a gamut conversion or choosing one profile.
    """
    if (not isinstance(images,(list,tuple)) or not 2<=len(images)<=30
            or not isinstance(rgbs,(list,tuple)) or not isinstance(alphas,(list,tuple))
            or len(rgbs)!=len(images) or len(alphas)!=len(images) or not callable(verify_icc)):
        raise ValueError('MULTI_SOURCE_INVALID')
    owned=tuple(WorkingImage.model_validate_json(canonical_bytes(image)) for image in images)
    shapes=_shapes([(image.canvas.height,image.canvas.width) for image in owned])
    color=owned[0].color
    for image,rgb,alpha,shape in zip(owned,rgbs,alphas,shapes):
        if (image.dtype!='float32' or image.color!=color or not isinstance(rgb,np.ndarray)
                or rgb.dtype!=np.float32 or rgb.shape!=(*shape,3)
                or (image.alpha is None)!=(alpha is None)):
            raise ValueError('MULTI_SOURCE_INVALID')
    if (color.profile_id,color.encoding)==('linear_srgb_v1','linear'):luma=(.2126,.7152,.0722)
    elif (color.profile_id,color.encoding)==('work_rec2020_gamma24_v1','gamma24'):luma=(.2627,.6780,.0593)
    else:raise ValueError('OPTICAL_COLOR_UNSUPPORTED')
    required=multi_source_resources(shapes,tile=tile)
    if type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:raise ValueError('MULTI_SOURCE_RAM_INVALID')
    if ram_max_bytes<required['ram_bytes']:raise ValueError('MULTI_SOURCE_RAM_EXCEEDED')
    if scratch.budget_bytes<required['scratch_bytes']:raise ValueError('MULTI_SOURCE_SCRATCH_EXCEEDED')
    check_active()
    if verify_icc(color) is not True:raise ValueError('MULTI_ICC_MISMATCH')
    # Reserve RGB files separately; normalized_alpha owns/releases its own files.
    rgb_budget=12*sum(h*w for h,w in shapes)+len(owned)*4096
    with ExitStack() as lifetime:
        reservation=lifetime.enter_context(scratch.reserve(rgb_budget))
        linear=[];opacity=[]
        for ordinal,(rgb,alpha,shape) in enumerate(zip(rgbs,alphas,shapes)):
            check_active()
            a=lifetime.enter_context(normalized_alpha(alpha,shape,scratch,tile=tile,check_active=check_active))
            path=reservation.path('multi-linear-'+str(ordinal),'.npy')
            value=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=rgb.shape)
            lifetime.callback(value._mmap.close)
            height,width=shape
            for y in range(0,height,tile):
                for x in range(0,width,tile):
                    check_active();value[y:y+tile,x:x+tile]=to_working_linear(rgb[y:y+tile,x:x+tile],color)
            value.flush();linear.append(value);opacity.append(a)
        reservation.validate();check_active()
        if verify_icc(color) is not True:raise ValueError('MULTI_ICC_MISMATCH')
        yield LinearMultiSources(owned,tuple(linear),tuple(opacity),color,luma)
