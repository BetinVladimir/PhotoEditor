"""Exact versioned ICC identity for approved exposure worker/adopter installs."""
from hashlib import sha256
from photo_worker.color.icc import ICCRegistry, ICCError
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import ColorSpace

WORKING_PROFILE_ID = 'work_rec2020_gamma24_v1'
WORKING_PROFILE_SHA = 'a525b6d4afd90340bc8f69535dae83594925224f3569a3b90b988ac5f937e6e9'


def local_exposure_icc_verifier(registry):
    """Require actual locked profile bytes, then verify each color metadata value.

    This is profile identity, not a scientific qualification or source decode
    proof. Runtime/SQL admission remains separately mandatory. No alternative
    profile or arbitrary checksum is accepted, even with a modified lock file.
    """
    if not isinstance(registry, ICCRegistry):
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')

    def profile_ready():
        try:
            profile = registry.resolve(WORKING_PROFILE_ID)
        except (ICCError, OSError, ValueError, KeyError, TypeError) as error:
            raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED') from error
        if (profile.id != WORKING_PROFILE_ID or profile.sha256 != WORKING_PROFILE_SHA
                or len(profile.data) != 648 or sha256(profile.data).hexdigest() != WORKING_PROFILE_SHA):
            raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')

    profile_ready()

    def verify(color):
        profile_ready()
        try:
            owned = ColorSpace.model_validate_json(canonical_bytes(color))
        except (ValueError, TypeError):
            return False
        return (owned.profile_id == WORKING_PROFILE_ID
                and owned.icc_sha256 == WORKING_PROFILE_SHA and owned.encoding == 'gamma24')

    return verify
