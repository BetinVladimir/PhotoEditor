"""Prepared donor patch to staged typed closure; not task installation/adoption."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from .portrait_open_eyes_material import prepare_open_eyes_material_patch
from photo_worker.operations.portrait.donor_files import write_donor_patch_files
from photo_worker.operations.portrait.donor_result import stage_donor_result


def prepare_and_stage_open_eyes_patch(params,source_raw,donor_raw,confirmation_raw,
        source_faces_raw,donor_faces_raw,reader,mask,protected,artifacts,scratch,*,
        source_origin,min_face_edge_px,material_scratch_root,material_max_bytes,
        check_active,authority,alpha_mode):
    """Caller supplies admitted physical masks and sufficient filter halo/core.

    Source/donor arrays are verified by reader; output files remain reserved
    through staging. Caller must account combined material/output scratch and
    implement current attempt/consent authority, orphan cleanup and adoption.
    """
    if not callable(check_active) or not callable(authority):
        raise ValueError('DONOR_AUTHORITY')
    if (not isinstance(mask,np.ndarray) or mask.ndim!=2 or min(mask.shape)<1 or
            mask.size>1_048_576 or not isinstance(protected,np.ndarray) or
            protected.shape!=mask.shape):
        raise ValueError('DONOR_PATCH')
    if not isinstance(params,PortraitOpenEyesParams):
        raise ValueError('DONOR_PARAMS')
    for value in (mask,protected):
        if (value.dtype!=np.float32 or not np.isfinite(value).all() or
                np.any(value<0) or np.any(value>1)):
            raise ValueError('DONOR_MASK')
    owned=PortraitOpenEyesParams.model_validate_json(canonical_bytes(params))
    source,donor,proof,sfaces,dfaces=map(bytes,
        (source_raw,donor_raw,confirmation_raw,source_faces_raw,donor_faces_raw))
    target,guard=mask.copy(),protected.copy()
    target.setflags(write=False);guard.setflags(write=False)
    def admit():
        check_active()
        if authority(owned.model_copy(deep=True),proof) is not True:
            raise ValueError('DONOR_AUTHORITY')
        check_active()
    with prepare_open_eyes_material_patch(owned,source,donor,proof,sfaces,dfaces,
            reader,target,guard,source_origin=source_origin,min_face_edge_px=min_face_edge_px,
            scratch_root=material_scratch_root,max_bytes=material_max_bytes,
            check_active=check_active,authority=authority,alpha_mode=alpha_mode) as (patch,material):
        with write_donor_patch_files(material.source_rgb,patch,source_origin,scratch,
                target=target,protected=guard,check_active=check_active) as files:
            admit()
            result=stage_donor_result(artifacts,owned,source,files,
                color_warning=patch.color.warning,check_active=admit)
            admit()
    return result
