"""Exact binary RGB change evidence for unpublished negative output mappings."""
import numpy as np


def write_negative_changed_mask(source, output, changed, *, check_active, ram_max_bytes):
    if not callable(check_active):raise ValueError('NEGATIVE_ACTIVITY_REQUIRED')
    if (any(not isinstance(a,np.ndarray) or a.dtype!=np.float32 for a in (source,output,changed))
            or source.ndim!=3 or source.shape[-1]!=3 or min(source.shape[:2])<1
            or source.shape[0]*source.shape[1]>200_000_000
            or output.shape!=source.shape or changed.shape!=source.shape[:2]
            or not changed.flags.writeable or np.shares_memory(changed,source)
            or np.shares_memory(changed,output)):
        raise ValueError('NEGATIVE_CHANGED_MASK_INVALID')
    pixels=source.shape[0]*source.shape[1]
    required=28*pixels+16*min(pixels,512*512)+65536
    if type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:
        raise ValueError('NEGATIVE_RESOURCE_INVALID')
    if ram_max_bytes<required:raise ValueError('NEGATIVE_RAM_EXCEEDED')
    count=0;check_active()
    for y in range(0,source.shape[0],512):
        for x in range(0,source.shape[1],512):
            check_active();tile=np.s_[y:y+512,x:x+512]
            a,b=source[tile],output[tile]
            if not np.isfinite(a).all() or not np.isfinite(b).all():
                raise ValueError('NEGATIVE_CHANGED_MASK_INVALID')
            # Signed-zero changes are observable in exact replay, too.
            mask=np.any(a.view(np.uint32)!=b.view(np.uint32),axis=-1)
            check_active();changed[tile]=mask
            count+=int(np.count_nonzero(mask))
    check_active();return count
