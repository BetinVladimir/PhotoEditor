"""Build-time LCMS installation from the uv-locked Linux Pillow wheel only.

No system-library discovery, downloads or dependency installation takes place.
The selected wheel member must match its own RECORD before it can be loaded.
"""
from __future__ import annotations

import base64
import hashlib
from importlib import metadata
import json
from pathlib import Path, PurePosixPath

MAX_LIBRARY_BYTES = 32 * 1024 * 1024


def wheel_library(distribution) -> tuple[Path, str]:
    candidates = []
    for member in distribution.files or ():
        relative = PurePosixPath(str(member))
        if (len(relative.parts) == 2 and relative.parts[0] == 'pillow.libs'
                and relative.name.startswith('liblcms2') and '.so' in relative.name
                and '..' not in relative.parts):
            candidates.append(member)
    if len(candidates) != 1:
        raise ValueError('PILLOW_LCMS_WHEEL_MEMBER_REQUIRED')
    member = candidates[0]
    path = Path(distribution.locate_file(member))
    record = member.hash
    if (path.is_symlink() or not path.is_file() or record is None
            or record.mode != 'sha256' or member.size is None
            or not 0 < member.size <= MAX_LIBRARY_BYTES or path.stat().st_size != member.size):
        raise ValueError('PILLOW_LCMS_RECORD_REQUIRED')
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        count = 0
        while chunk := stream.read(1024 * 1024):
            count += len(chunk)
            if count > MAX_LIBRARY_BYTES:
                raise ValueError('PILLOW_LCMS_SIZE_INVALID')
            digest.update(chunk)
    actual = base64.urlsafe_b64encode(digest.digest()).decode('ascii').rstrip('=')
    if count != member.size or actual != record.value:
        raise ValueError('PILLOW_LCMS_RECORD_MISMATCH')
    return path.resolve(strict=True), digest.hexdigest()


def probe_api(path: Path) -> int:
    # Constructing the production adapter binds every required ctypes symbol and
    # signature. Actual profile creation/serialization/open exercises LCMS ABI.
    from photo_worker.color.icc import LittleCMS
    cms = LittleCMS(str(path))
    version = cms.version
    if not 2000 <= version < 3000:
        raise ValueError('LCMS2_VERSION_REQUIRED')
    profile = cms.generate('srgb_v1')
    handle, owned = cms.open(profile)
    try:
        if not owned or not handle:
            raise ValueError('LCMS2_PROFILE_API_FAILED')
    finally:
        cms.lib.cmsCloseProfile(handle)
    import numpy as np
    pixels = np.array([[[0, 32768, 65535]]], dtype=np.uint16)
    converted = cms.convert(pixels, profile, profile)
    if converted.shape != pixels.shape or converted.dtype != np.uint16:
        raise ValueError('LCMS2_TRANSFORM_API_FAILED')
    return version


def install(destination: Path, distribution=None, probe=probe_api) -> dict:
    distribution = distribution or metadata.distribution('Pillow')
    source, digest = wheel_library(distribution)
    version = probe(source)
    destination.mkdir(parents=True, exist_ok=True)
    target = destination / 'liblcms2.so'
    if target.exists() or target.is_symlink():
        raise ValueError('LCMS_INSTALL_TARGET_EXISTS')
    target.symlink_to(source)
    manifest = {'schema_version': 1, 'provider': 'locked_pillow_wheel',
                'pillow_version': distribution.version, 'library_path': str(target),
                'wheel_member_path': str(source), 'library_sha256': digest,
                'lcms_encoded_version': version}
    (destination / 'lcms-manifest.json').write_text(json.dumps(manifest, sort_keys=True) + '\n')
    return manifest


if __name__ == '__main__':
    install(Path('/opt/photo/native'))
