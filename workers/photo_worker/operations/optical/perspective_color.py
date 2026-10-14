"""Source-primary optical transfer identity against operator ICC registry."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import ColorSpace
from photo_worker.color.icc import ICCRegistry


def perspective_icc_verifier(registry):
    if not isinstance(registry,ICCRegistry):raise ValueError('PERSPECTIVE_ICC_UNQUALIFIED')
    def verify(color):
        owned=ColorSpace.model_validate_json(canonical_bytes(color))
        profile=registry.resolve(owned.profile_id)
        if (profile.id!=owned.profile_id or profile.sha256!=owned.icc_sha256 or
            sha256(profile.data).hexdigest()!=owned.icc_sha256):
            raise ValueError('PERSPECTIVE_ICC_MISMATCH')
        return True
    return verify
