"""Independent adoption-side C2PA and raster verification of immutable files."""
from contextlib import ExitStack
from .cancellation import with_signing_task, check_signing_active, bind_current_signing_task
from hashlib import sha256
from pathlib import Path
import os
import re
import stat

from photo_worker.contracts.provenance import SigningLineage, SigningPolicy, decode_provenance
from photo_worker.runtime.image_fingerprint import decoded_image_fingerprint
from .sdk import _file_hash, _same_staged_file
from .verify import verify_sdk_reader
from .container_manifest import verify_container_manifest


@with_signing_task
def verify_signed_files(source_path, signed_path, mime, source_sha, signed_sha,
                        manifest, manifest_sha, lineage_raw, policy_raw, operator,
                        *, sdk, check_active=lambda: None):
    """Verify staged bytes; caller still owns fenced metadata/SQL authority.

    operator.verify_reader must additionally prove the exact binary manifest
    matches the embedded store and expected pinned certificate chain. SDK JSON
    trust status alone cannot establish those two bindings.
    """
    if (mime not in ('image/jpeg', 'image/png') or not callable(check_active)
            or sdk is None or not callable(getattr(sdk, 'Reader', None))
            or any(not isinstance(value, str) or re.fullmatch(r'[0-9a-f]{64}', value) is None
                   for value in (source_sha, signed_sha, manifest_sha))
            or not isinstance(manifest, bytes) or not 1 <= len(manifest) <= 8 << 20
            or sha256(manifest).hexdigest() != manifest_sha
            or not Path(source_path).is_absolute() or not Path(signed_path).is_absolute()
            or Path(source_path) == Path(signed_path)):
        raise ValueError('SIGNER_VERIFICATION_INPUT')
    policy = decode_provenance(policy_raw, SigningPolicy, 1 << 20)
    lineage = decode_provenance(lineage_raw, SigningLineage, 8 << 20)
    if mime not in policy.allowed_mime:
        raise ValueError('SIGNER_VERIFICATION_MIME')
    bound_active = bind_current_signing_task(check_active)
    check_signing_active()
    operator.check_installation(policy)
    with ExitStack() as stack:
        streams = []
        for path, expected in ((source_path, source_sha), (signed_path, signed_sha)):
            descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            stream = stack.enter_context(os.fdopen(descriptor, 'rb', buffering=0))
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or not 1 <= info.st_size <= 512 << 20:
                raise ValueError('SIGNER_VERIFICATION_FILE')
            identity = (info.st_dev, info.st_ino)
            pins = _file_hash(stream, 512 << 20, bound_active)
            if pins != (expected, info.st_size):
                raise ValueError('SIGNER_VERIFICATION_HASH')
            streams.append((stream, pins, path, identity))
        source, signed = streams[0][0], streams[1][0]
        verify_container_manifest(signed, mime, manifest, check_active=bound_active)
        source.seek(0)
        original_pixels = decoded_image_fingerprint(source, mime, check_active=bound_active)
        signed.seek(0)
        if decoded_image_fingerprint(signed, mime, check_active=bound_active) != original_pixels:
            raise ValueError('SIGNER_VERIFICATION_PIXELS')
        context = stack.enter_context(operator.open_context(policy))
        signed.seek(0)
        with sdk.Reader(mime, signed, context=context) as reader:
            validation_sha = verify_sdk_reader(reader, policy.model_dump(mode='json'),
                                                 lineage.model_dump(mode='json'), mime,
                                                 check_active=bound_active)
            operator.verify_reader(reader, policy, lineage, manifest)
        for stream, pins, path, identity in streams:
            if _file_hash(stream, 512 << 20, bound_active) != pins:
                raise ValueError('SIGNER_VERIFICATION_CHANGED')
            if not _same_staged_file(path, identity):
                raise ValueError('SIGNER_VERIFICATION_PATH_CHANGED')
        operator.check_installation(policy)
        check_signing_active()
        return validation_sha
