"""Strict working-color verification against an installer-owned ICC fingerprint."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import AdoptionInvalid


def semantic_working_color_authority(working_icc_sha):
    """Fingerprint must come from the trusted ICC registry, never image metadata."""
    if (not isinstance(working_icc_sha,str) or len(working_icc_sha)!=64
            or any(character not in '0123456789abcdef' for character in working_icc_sha)):
        raise ValueError('SEMANTIC_ICC_CONFIGURATION_INVALID')
    def verify(image):
        image=WorkingImage.model_validate_json(canonical_bytes(image))
        if (image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
                or image.color.encoding!='gamma24' or image.color.icc_sha256!=working_icc_sha):
            raise AdoptionInvalid('SEMANTIC_WORKING_COLOR_INVALID')
    return verify
