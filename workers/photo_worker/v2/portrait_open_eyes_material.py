"""Physical RGB-backed patch processing; masks and live authority remain caller gates."""
from contextlib import contextmanager
from .portrait_donor_material import read_donor_image_material
from .portrait_open_eyes_prepare import prepare_open_eyes_manifest_patch


@contextmanager
def prepare_open_eyes_material_patch(params, source_image_raw, donor_image_raw,
        confirmation_raw, source_faces_raw, donor_faces_raw, reader, mask, protected,
        *, source_origin, min_face_edge_px, scratch_root, max_bytes,
        check_active, authority, alpha_mode):
    """Yield reviewed RGB patch plus material while mappings remain open.

    The caller must validate/rasterize masks and preserve source alpha during
    result construction. Sampled donor coverage is enforced. This is not an installed
    operation. Patch shape comes from the supplied mask, not a full-frame copy.
    """
    if not callable(authority) or not callable(check_active):
        raise ValueError('DONOR_AUTHORITY')
    owned=params.model_copy(deep=True)
    source_raw,donor_raw=bytes(source_image_raw),bytes(donor_image_raw)
    proof,source_faces,donor_faces=bytes(confirmation_raw),bytes(source_faces_raw),bytes(donor_faces_raw)
    if (not isinstance(source_origin,tuple) or len(source_origin)!=2 or
            any(type(v) is not int or v<0 for v in source_origin) or
            getattr(mask,'ndim',None)!=2 or min(mask.shape)<1 or
            mask.shape[0]*mask.shape[1]>1_048_576 or
            getattr(protected,'shape',None)!=mask.shape):
        raise ValueError('DONOR_PATCH')
    check_active()
    if authority(owned.model_copy(deep=True),proof) is not True:
        raise ValueError('DONOR_AUTHORITY')
    with read_donor_image_material(owned,source_raw,donor_raw,reader,
            scratch_root=scratch_root,max_bytes=max_bytes,check_active=check_active,
            alpha_mode=alpha_mode) as material:
        x,y=source_origin
        height,width=mask.shape
        if y+height>material.source_rgb.shape[0] or x+width>material.source_rgb.shape[1]:
            raise ValueError('DONOR_CANVAS')
        patch=material.source_rgb[y:y+height,x:x+width]
        alpha_patch=None if material.source_alpha is None else material.source_alpha[y:y+height,x:x+width]
        result=prepare_open_eyes_manifest_patch(owned,source_raw,donor_raw,proof,
            source_faces,donor_faces,patch,material.donor_rgb,mask,protected,
            source_origin,min_face_edge_px,check_active,authority,
            donor_alpha=material.donor_alpha,source_alpha=alpha_patch)
        yield result,material
        check_active()
