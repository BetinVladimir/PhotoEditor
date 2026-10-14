"""Structural V28 input closure checks; tenant/version authority is caller-owned."""
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams


def verify_local_inputs(params, source: WorkingImage, target: MaskRef,
                        protected: MaskRef, declared_inputs):
    if not isinstance(params, (PortraitHairParams, PortraitClothingParams)):
        raise ValueError('PORTRAIT_PARAMS_INVALID')
    if source.dtype != 'float32' or canonical_hash(source.canvas) != params.base.canvas_hash:
        raise ValueError('PORTRAIT_SOURCE_MISMATCH')
    if source.canvas.width * source.canvas.height > 200000000:
        raise ValueError('PORTRAIT_PIXEL_LIMIT')
    # Transfer decoding alone cannot provide an arbitrary ICC conversion.
    if source.color.encoding != 'linear' and not (
            source.color.encoding == 'gamma24' and
            source.color.profile_id == 'work_rec2020_gamma24_v1'):
        raise ValueError('PORTRAIT_COLOR_UNSUPPORTED')
    if target.protected or not protected.protected:
        raise ValueError('PORTRAIT_MASK_ROLE_MISMATCH')
    for mask in (target, protected):
        if mask.canvas != source.canvas or mask.transform_chain != source.transform_chain:
            raise ValueError('PORTRAIT_MASK_GEOMETRY_MISMATCH')
    required = [params.base.source, source.pixels, params.mask, params.protected,
                target.artifact, protected.artifact]
    if source.alpha is not None:
        required.append(source.alpha)
    if any(ref not in declared_inputs for ref in required):
        raise ValueError('PORTRAIT_INPUT_UNDECLARED')
