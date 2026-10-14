"""Physical original-manual proof; caller owns SQL lineage and pinned byte IO."""
from hashlib import sha256

from photo_worker.contracts.base import ArtifactRef,parse_json
from photo_worker.contracts.manual_mask import MaskRasterParams,ManualMask,RasterizedMask
from photo_worker.operations.mask.verify import verify_raster


def verify_manual_pair(source_ref:ArtifactRef,source,params_ref:ArtifactRef,params_raw:bytes,
                       root:RasterizedMask,target,guard,target_pixels,guard_pixels,
                       *,vectors_raw=None,check_active=lambda:None):
    check_active()
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=1048576 or sha256(params_raw).hexdigest()!=params_ref.sha256:
        raise ValueError('PORTRAIT_MANUAL_PARAMS_MISMATCH')
    parse_json(params_raw)
    params=MaskRasterParams.model_validate_json(params_raw)
    if (params.image!=source_ref or root.image!=source_ref or root.target!=target or
            root.protected!=guard or source.transform_chain.steps):
        raise ValueError('PORTRAIT_MANUAL_ROOT_MISMATCH')
    expected_vectors=params.vectors if params.vectors is not None else params_ref
    if root.vectors!=expected_vectors:
        raise ValueError('PORTRAIT_MANUAL_VECTORS_MISMATCH')
    if params.vectors is not None:
        if not isinstance(vectors_raw,bytes) or not 2<=len(vectors_raw)<=1048576 or sha256(vectors_raw).hexdigest()!=params.vectors.sha256:
            raise ValueError('PORTRAIT_MANUAL_VECTORS_MISMATCH')
        parse_json(vectors_raw)
        vectors=ManualMask.model_validate_json(vectors_raw)
    else:
        if vectors_raw is not None:
            raise ValueError('PORTRAIT_MANUAL_VECTORS_MISMATCH')
        vectors=params.inline_vectors
    if vectors.canvas!=source.canvas or vectors.source_sha256!=source.canvas.source_sha256:
        raise ValueError('PORTRAIT_MANUAL_SOURCE_MISMATCH')
    for mask,protected in ((target,False),(guard,True)):
        if (mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain or
                mask.object_id!=params.object_id or mask.selector!='manual' or
                mask.confidence!=1 or mask.model_lock_id is not None or mask.protected!=protected):
            raise ValueError('PORTRAIT_MANUAL_MASK_MISMATCH')
    return verify_raster(vectors,source.canvas.source_sha256,target_pixels,guard_pixels,
                         tile=512,check_active=check_active)
