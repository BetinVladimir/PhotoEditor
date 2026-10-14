"""Versioned linear sRGB ICC identity for negative worker installation."""
from hashlib import sha256
from photo_worker.color.icc import ICCRegistry
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage

LINEAR_PROFILE_SHA='25f7241171e92bfb99c2172440a55f714875d9caf9891f8a29a32368e0f15acc'


def negative_icc_verifier(registry):
    if not isinstance(registry,ICCRegistry):raise ValueError('NEGATIVE_ICC_UNQUALIFIED')
    def ready():
        profile=registry.resolve('linear_srgb_v1')
        if profile.id!='linear_srgb_v1' or profile.sha256!=LINEAR_PROFILE_SHA or len(profile.data)!=628 or sha256(profile.data).hexdigest()!=LINEAR_PROFILE_SHA:
            raise ValueError('NEGATIVE_ICC_UNQUALIFIED')
    ready()
    def verify(image):
        ready();owned=WorkingImage.model_validate_json(canonical_bytes(image))
        return (owned.color.profile_id=='linear_srgb_v1' and owned.color.encoding=='linear'
            and owned.color.icc_sha256==LINEAR_PROFILE_SHA)
    return verify
