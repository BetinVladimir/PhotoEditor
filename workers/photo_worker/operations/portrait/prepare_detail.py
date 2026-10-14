"""Scoped native detail pixels; no staging or report publication."""
from contextlib import contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.algorithms.portrait_detail import apply_region
from photo_worker.contracts.portrait_detail import DetailEvidence


@dataclass(frozen=True)
class PreparedDetail:
    rgb: object
    regions: tuple[DetailEvidence,...]


def detail_peak(width,height):
    """Full-frame native temporaries; this is not runtime qualification."""
    if type(width) is not int or type(height) is not int or not 1<=width*height<=200000000 or min(width,height)<1:
        raise ValueError('PORTRAIT_DETAIL_INPUT_INVALID')
    return {'ram_bytes':max(1073741824,width*height*160+1048576),
        'scratch_bytes':width*height*12+8192}


@contextmanager
def prepare_detail(rgb,regions,detail,read_mask,protected,*,check_active=lambda:None):
    """Preserve authored region ordering and native linear-light adjustments.

    Mask callback owners must remain live through yield. No detection, mask
    inference, confirmation or model qualification is performed here.
    """
    from copy import deepcopy
    from photo_worker.contracts.portrait_detail import DetailRegion
    from photo_worker.contracts.style import PortraitDetail
    regions=deepcopy(tuple(regions));detail=deepcopy(detail)
    if (not isinstance(detail,PortraitDetail) or not detail.enabled or len(regions)>128
            or any(not isinstance(region,DetailRegion) for region in regions)
            or len({region.region_id for region in regions})!=len(regions)
            or not callable(read_mask) or not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32
            or rgb.ndim!=3 or rgb.shape[2]!=3 or not 1<=rgb.shape[0]*rgb.shape[1]<=200000000
            or not isinstance(protected,np.ndarray) or protected.dtype!=np.float32
            or protected.shape!=rgb.shape[:2]):
        raise ValueError('PORTRAIT_DETAIL_INPUT_INVALID')
    from photo_worker.contracts.base import canonical_bytes
    detail=PortraitDetail.model_validate_json(canonical_bytes(detail))
    regions=tuple(DetailRegion.model_validate_json(canonical_bytes(region)) for region in regions)
    check_active()
    # Finite/unit checks are tiled to avoid whole-image boolean temporaries.
    def validate(array):
        for y in range(0,array.shape[0],256):
            check_active();tile=array[y:y+256]
            if not np.all(np.isfinite(tile)) or np.any(tile<0) or np.any(tile>1):
                raise ValueError('PORTRAIT_DETAIL_INPUT_INVALID')
    validate(rgb);validate(protected)
    current=rgb.copy();reports=[]
    for region in sorted(regions,key=lambda entry:entry.region_id):
        check_active();mask=read_mask(region.mask.artifact)
        if not isinstance(mask,np.ndarray) or mask.dtype!=np.float32 or mask.shape!=rgb.shape[:2]:
            raise ValueError('PORTRAIT_DETAIL_MASK_INVALID')
        validate(mask)
        base={'teeth':detail.teeth_strength,'sclera':detail.teeth_strength,'shine':detail.shine_strength,
            'under_eye':detail.under_eye_strength,'cloth':region.strength,'glasses':region.strength}[region.kind]
        strength=base*region.strength if region.kind not in ('cloth','glasses') else base
        current,changed,max_ev,chroma,reason=apply_region(current,mask,region.kind,strength,protected,check_active)
        reports.append(DetailEvidence(region_id=region.region_id,kind=region.kind,applied=reason is None,
            changed_pixels=changed,max_ev=max_ev,chroma_reduction=chroma,reason=reason))
    current.flags.writeable=False
    check_active();yield PreparedDetail(current,tuple(reports));check_active()
