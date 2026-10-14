"""Pinned V28 source/donor image documents, not physical pixel authority."""
import hashlib
from photo_worker.contracts.base import canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.images import WorkingImage


def verify_donor_working_image(raw: bytes, binding) -> WorkingImage:
    parse_json(raw, max_bytes=1_048_576)
    if hashlib.sha256(raw).hexdigest() != binding.source.sha256:
        raise ValueError('DONOR_IMAGE_SHA')
    image = WorkingImage.model_validate_json(raw)
    if canonical_bytes(image) != raw or canonical_hash(image.canvas) != binding.canvas_hash:
        raise ValueError('DONOR_IMAGE_BINDING')
    if (image.dtype != 'float32' or image.canvas.width * image.canvas.height > 200_000_000 or
            image.pixels.artifact_id.int == 0 or (image.alpha is not None and image.alpha.artifact_id.int == 0)):
        raise ValueError('DONOR_IMAGE_FORMAT')
    return image


def verify_donor_image_pair(params, source_raw: bytes, donor_raw: bytes):
    source = verify_donor_working_image(source_raw, params.base)
    donor = verify_donor_working_image(donor_raw, params.donor)
    if source.color != donor.color:
        raise ValueError('DONOR_COLOR_SPACE')
    return source, donor
