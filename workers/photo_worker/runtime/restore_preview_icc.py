"""Preview ICC authority from explicitly installed production registry."""
from hashlib import sha256
from photo_worker.color.icc import ICCRegistry,validate_icc
from .restore_authority import require_restore_active


def verify_preview_icc(registry,source,srgb_profile,*,check_active):
    if not isinstance(registry,ICCRegistry) or type(srgb_profile) is not bytes or not callable(check_active):
        raise ValueError('RESTORE_PREVIEW_ICC_INSTALLATION')
    require_restore_active(check_active)
    working=registry.resolve(source.profile_id);target=registry.resolve('srgb_v1')
    validate_icc(working.data);validate_icc(target.data)
    if (working.sha256!=source.icc_sha256 or sha256(working.data).hexdigest()!=source.icc_sha256
            or target.data!=srgb_profile or sha256(target.data).hexdigest()!=target.sha256):
        raise ValueError('RESTORE_PREVIEW_ICC_BINDING')
    require_restore_active(check_active);return (working.sha256,target.sha256)
