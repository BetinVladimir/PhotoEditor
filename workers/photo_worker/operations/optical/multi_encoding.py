"""Verified transfer restoration and changed-mask files for multi-input output."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import ColorSpace
from photo_worker.runtime.scratch import Reservation
from .color import from_working_linear


@dataclass(frozen=True)
class EncodedMultiFiles:
    rgb: Path
    alpha: Path
    changed_mask: Path
    reservation: Reservation


def multi_encoding_resources(output_shape, reference_shape, *, tile=256):
    for shape in (output_shape,reference_shape):
        if (not isinstance(shape,tuple) or len(shape)!=2 or any(type(v) is not int or v<1 for v in shape)
                or shape[0]*shape[1]>200000000):raise ValueError('MULTI_ENCODING_INVALID')
    if type(tile) is not int or not 1<=tile<=512:raise ValueError('MULTI_ENCODING_INVALID')
    pixels=output_shape[0]*output_shape[1];reference=reference_shape[0]*reference_shape[1]
    output=20*pixels+3*4096
    return dict(pixel_count=pixels,ram_bytes=16*reference+16*pixels+output+128*tile*tile+256*1024*1024,
                scratch_bytes=output,output_bytes=output,vram_bytes=0)


@contextmanager
def prepare_multi_encoding(linear,alpha,reference_rgb,reference_alpha,color,scratch,*,
        geometry_changed,verify_icc,ram_max_bytes,tile=256,check_active=lambda:None):
    """Restore the verified source encoding; never relabel linear RGB gamma24.

    Geometry-changed output uses a full changed mask. Otherwise compare encoded
    RGB and opacity bits on the unchanged reference canvas. The adapter derives
    geometry_changed from retained reference transforms, including projection;
    this helper alone grants no geometry, source or qualification authority.
    """
    if (not isinstance(linear,np.ndarray) or linear.dtype!=np.float32 or linear.ndim!=3 or linear.shape[2]!=3
            or not isinstance(alpha,np.ndarray) or alpha.dtype!=np.float32 or alpha.shape!=linear.shape[:2]
            or not isinstance(reference_rgb,np.ndarray) or reference_rgb.dtype!=np.float32 or reference_rgb.ndim!=3 or reference_rgb.shape[2]!=3
            or not isinstance(reference_alpha,np.ndarray) or reference_alpha.dtype!=np.float32 or reference_alpha.shape!=reference_rgb.shape[:2]
            or type(geometry_changed) is not bool or not callable(verify_icc)):
        raise ValueError('MULTI_ENCODING_INVALID')
    if not geometry_changed and linear.shape!=reference_rgb.shape:raise ValueError('MULTI_ENCODING_INVALID')
    owned=ColorSpace.model_validate_json(canonical_bytes(color))
    if (owned.profile_id,owned.encoding) not in {('linear_srgb_v1','linear'),('work_rec2020_gamma24_v1','gamma24')}:
        raise ValueError('OPTICAL_COLOR_UNSUPPORTED')
    required=multi_encoding_resources(alpha.shape,reference_alpha.shape,tile=tile)
    if type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:raise ValueError('MULTI_ENCODING_RAM_INVALID')
    if ram_max_bytes<required['ram_bytes']:raise ValueError('MULTI_ENCODING_RAM_EXCEEDED')
    check_active()
    if verify_icc(owned) is not True:raise ValueError('MULTI_ICC_MISMATCH')
    # Validate the complete reference, including areas lost by the new geometry.
    rh,rw=reference_alpha.shape
    for y in range(0,rh,tile):
        for x in range(0,rw,tile):
            check_active();a=reference_alpha[y:y+tile,x:x+tile]
            if not np.isfinite(reference_rgb[y:y+tile,x:x+tile]).all() or not np.isfinite(a).all() or np.any(a<0) or np.any(a>1):
                raise ValueError('MULTI_ENCODING_INVALID')
    with scratch.reserve(required['scratch_bytes']) as reservation:
        with ExitStack() as mappings:
            def allocate(role,shape):
                path=reservation.path('multi-encoded-'+role,'.npy')
                value=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=shape)
                mappings.callback(value._mmap.close)
                return path,value
            rgb_path,colors=allocate('rgb',linear.shape)
            alpha_path,opacity=allocate('alpha',alpha.shape)
            mask_path,mask=allocate('changed',alpha.shape)
            height,width=alpha.shape
            for y in range(0,height,tile):
                for x in range(0,width,tile):
                    check_active();part=np.s_[y:y+tile,x:x+tile];a=alpha[part]
                    if not np.isfinite(a).all() or np.any(a<0) or np.any(a>1):raise ValueError('MULTI_ENCODING_INVALID')
                    encoded=from_working_linear(linear[part],owned)
                    colors[part]=encoded;opacity[part]=a
                    if geometry_changed:mask[part]=1
                    else:mask[part]=(np.any(encoded.view(np.uint32)!=reference_rgb[part].view(np.uint32),axis=-1)|
                                     (a.view(np.uint32)!=reference_alpha[part].view(np.uint32))).astype(np.float32)
            for value in (colors,opacity,mask):value.flush()
            reservation.validate();check_active()
            if verify_icc(owned) is not True:raise ValueError('MULTI_ICC_MISMATCH')
        yield EncodedMultiFiles(rgb_path,alpha_path,mask_path,reservation)
