"""C2PA Builder/Reader bridge; qualified operator owns keys, trust and networking.

API reference: https://github.com/contentauth/c2pa-python/blob/main/docs/usage.md
No registration/dispatch is implied by this module. Never accept operator or
SDK dependencies from task JSON. Production must install a qualified SDK lock.
"""
from contextlib import ExitStack
from .cancellation import with_signing_task, check_signing_active, bind_current_signing_task
from hashlib import sha256
from pathlib import Path
import os
import stat

from photo_worker.contracts.provenance import SigningPolicy, SigningLineage, decode_provenance
from photo_worker.runtime.image_fingerprint import decoded_image_fingerprint as _pixels
from .verify import verify_sdk_reader
from .bounded_stream import BoundedSigningStream
from .container_manifest import verify_container_manifest


def _file_hash(stream, maximum, check_active=lambda: None):
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    stream.seek(0)
    guard()
    digest, size = sha256(), 0
    while True:
        guard()
        chunk = stream.read(min(1 << 20, maximum + 1 - size))
        guard()
        if not chunk:
            break
        size += len(chunk)
        if size > maximum:
            raise ValueError('SIGNER_FILE_LIMIT')
        digest.update(chunk)
    guard()
    return digest.hexdigest(), size


def _same_staged_file(path, identity):
    try:
        current = os.stat(path, follow_symlinks=False)
    except FileNotFoundError:
        return False
    return (stat.S_ISREG(current.st_mode) and current.st_nlink == 1
            and (current.st_dev, current.st_ino) == identity)


@with_signing_task
def sign_staged(input_path, output_path, mime, source_sha, lineage_raw,
                policy_raw, operator, *, sdk=None, check_active=lambda: None):
    """Return verified binary C2PA manifest and pins, never publish outputs.

    operator.check_installation(policy) must prove exact installed SDK lock,
    certificate/trust pins, live key and allowed timestamp/revocation policy.
    operator.open_context/open_signer return qualified SDK context managers.
    operator.verify_reader must check trusted hard binding, certificate chain,
    exact custom lineage assertion and timestamp/revocation requirements; an
    SDK-readable manifest alone is not successful verification.
    """
    if (not callable(check_active) or mime not in ('image/jpeg', 'image/png')
            or not isinstance(lineage_raw, bytes) or not 2 <= len(lineage_raw) <= 8 << 20
            or not isinstance(policy_raw, bytes) or not 2 <= len(policy_raw) <= 1 << 20
            or not isinstance(source_sha, str) or len(source_sha) != 64
            or not Path(input_path).is_absolute() or not Path(output_path).is_absolute()
            or input_path == output_path):
        raise ValueError('SIGNER_INPUT')
    lineage = decode_provenance(lineage_raw, SigningLineage, 8 << 20)
    policy = decode_provenance(policy_raw, SigningPolicy, 1 << 20)
    if mime not in policy.allowed_mime:
        raise ValueError('SIGNER_POLICY_MIME')
    bound_active = bind_current_signing_task(check_active)
    check_signing_active()
    operator.check_installation(policy)
    check_signing_active()
    if sdk is None:
        import c2pa as sdk
    definition = {'claim_generator_info': [{'name': 'PhotoEditor', 'version': '1'}],
                  'format': mime, 'assertions': [
                      {'label': 'org.photoeditor.lineage', 'data': lineage.model_dump(mode='json')}]}
    created = False
    output_identity = None
    try:
        with ExitStack() as stack:
            check_signing_active()
            fd = os.open(input_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            source = stack.enter_context(os.fdopen(fd, 'rb'))
            check_signing_active()
            info = os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or not 1 <= info.st_size <= 512 << 20:
                raise ValueError('SIGNER_SOURCE_FILE')
            source_identity = (info.st_dev, info.st_ino)
            before_hash, before_size = _file_hash(source, 512 << 20, bound_active)
            if before_hash != source_sha or before_size != info.st_size:
                raise ValueError('SIGNER_SOURCE_HASH')
            pixels = _pixels(source, mime, check_active=bound_active)
            source.seek(0)
            check_signing_active()
            fd = os.open(output_path, os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_RDWR, 0o600)
            created = True
            output_info = os.fstat(fd)
            output_identity = (output_info.st_dev, output_info.st_ino)
            output = stack.enter_context(os.fdopen(fd, 'w+b'))
            check_signing_active()
            context = stack.enter_context(operator.open_context(policy))
            signer = stack.enter_context(operator.open_signer(policy))
            check_signing_active()
            with sdk.Builder(definition, context) as builder:
                with BoundedSigningStream(output, 512 << 20, check_active=bound_active) as destination:
                    with BoundedSigningStream(source, 512 << 20, check_active=bound_active) as source_stream:
                        manifest = builder.sign(signer, mime, source_stream, destination)
                check_signing_active()
            if not isinstance(manifest, bytes) or not 1 <= len(manifest) <= 8 << 20:
                raise ValueError('SIGNER_BINARY_MANIFEST')
            output.flush()
            os.fsync(output.fileno())
            verify_container_manifest(output, mime, manifest, check_active=bound_active)
            output_sha, output_size = _file_hash(output, 512 << 20, bound_active)
            if _pixels(output, mime, check_active=bound_active) != pixels:
                raise ValueError('SIGNER_PIXELS_CHANGED')
            output.seek(0)
            check_signing_active()
            with sdk.Reader(mime, output, context=context) as reader:
                check_signing_active()
                verification_sha = verify_sdk_reader(reader, policy.model_dump(mode='json'),
                    lineage.model_dump(mode='json'), mime, check_active=bound_active)
                operator.verify_reader(reader, policy, lineage, manifest)
                check_signing_active()
            check_signing_active()
            operator.check_installation(policy)
            if _file_hash(source, 512 << 20, bound_active) != (before_hash, before_size):
                raise ValueError('SIGNER_SOURCE_CHANGED')
            if _file_hash(output, 512 << 20, bound_active) != (output_sha, output_size):
                raise ValueError('SIGNER_OUTPUT_CHANGED')
            if (not _same_staged_file(input_path, source_identity)
                    or not _same_staged_file(output_path, output_identity)):
                raise ValueError('SIGNER_STAGED_FILE_CHANGED')
            check_signing_active()
            return {'manifest': manifest, 'manifest_sha256': sha256(manifest).hexdigest(),
                    'signed_sha256': output_sha, 'signed_bytes': output_size,
                    'validation_sha256': verification_sha}
    except BaseException:
        if created and output_identity is not None and _same_staged_file(output_path, output_identity):
            os.unlink(output_path)
        raise
