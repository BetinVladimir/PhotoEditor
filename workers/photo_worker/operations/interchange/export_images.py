"""Physical encoded-image qualification for frozen interchange exports."""
from pathlib import Path, PurePosixPath
from hashlib import sha256
import stat
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.asset.verify import (VerifiedSource, verify_magic,
    verify_suffix, verify_raster_decode)
from photo_worker.operations.asset.inspect import read_metadata
from photo_worker.v2.interchange_export_sidecars import ExportImageEvidence


def qualify_export_image(entry, reader, scratch, *, active=lambda: None):
    return _qualify_export_image(entry, reader, scratch, 'EncodedImage', active=active)


def qualify_prepared_background(reference, media_type, reader, scratch, *, active=lambda: None):
    """Reuse the complete physical decoder for an admitted import input."""
    from types import SimpleNamespace
    suffix = {'image/jpeg': '.jpg', 'image/png': '.png', 'image/tiff': '.tiff'}.get(media_type)
    if suffix is None:
        raise ValueError('INTERCHANGE_BACKGROUND_MEDIA')
    return _qualify_export_image(SimpleNamespace(content=reference, path='background' + suffix),
                                reader, scratch, 'StyleBackgroundAsset', active=active)


def qualify_export_background(pin, reader, scratch, *, active=lambda: None):
    from types import SimpleNamespace
    from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
    pin = FrozenBackgroundAsset.model_validate_json(canonical_bytes(pin))
    active()
    evidence = reader.evidence(pin.source)
    if evidence.bytes != pin.expected_bytes or evidence.media_type != pin.media_type:
        raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_PIN')
    entry = SimpleNamespace(content=pin.source, path='background' + pin.suffix)
    result = _qualify_export_image(entry, reader, scratch, 'StyleBackgroundAsset', active=active)
    if reader.evidence(pin.source) != evidence:
        raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_CHANGED')
    active()
    return result


def _qualify_export_image(entry, reader, scratch, contract, *, active):
    reference = ArtifactRef.model_validate_json(canonical_bytes(entry.content))
    suffix = PurePosixPath(entry.path).suffix.lower()
    if suffix not in ('.tif', '.tiff', '.jpg', '.jpeg', '.png') or not callable(active):
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_FORMAT')
    declared = reader.entries.get((reference.artifact_id, reference.sha256))
    if declared is None or declared.contract != contract:
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_CONTRACT')
    evidence = reader.evidence(reference)
    if evidence.media_type not in ('image/tiff', 'image/jpeg', 'image/png'):
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_MEDIA')
    active()
    with scratch.reserve(evidence.bytes) as reservation:
        path = reservation.path('export-image', suffix)
        with scratch.open_writer(path, reservation) as output:
            for block in reader.iter_bytes(reference, evidence.bytes):
                active()
                if output.write(block) != len(block):
                    raise OSError('short export image write')
        reservation.validate_file(path)
        metadata = qualify_export_image_file(path, evidence.media_type,
            evidence.bytes, reference.sha256, active=active)
        active()
        # Recheck the immutable gateway pin after native decoder IO.
        if reader.evidence(reference) != evidence:
            raise ValueError('INTERCHANGE_EXPORT_IMAGE_CHANGED')
        return ExportImageEvidence(reference, evidence.bytes, evidence.media_type, metadata.orientation)


def qualify_export_image_file(path, media_type, expected_bytes, expected_sha, *, active=lambda: None):
    """Native qualification shared by worker and admission bridge.

    The caller owns a private staged file; this function independently checks its
    physical bytes, magic, suffix, ICC/metadata and complete raster decode.
    """
    if (not isinstance(path, Path) or type(expected_bytes) is not int
            or not 1 <= expected_bytes <= 64 << 30
            or not isinstance(expected_sha, str) or len(expected_sha) != 64
            or any(c not in '0123456789abcdef' for c in expected_sha)
            or media_type not in ('image/tiff', 'image/jpeg', 'image/png')
            or not callable(active)):
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_INPUT')
    active()
    before = path.lstat()
    if not stat.S_ISREG(before.st_mode) or before.st_size != expected_bytes:
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_FILE')
    pin = (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
    digest, count = sha256(), 0
    with path.open('rb') as source:
        while True:
            active()
            block = source.read(256 << 10)
            if not block:
                break
            count += len(block)
            if count > expected_bytes:
                raise ValueError('INTERCHANGE_EXPORT_IMAGE_SIZE')
            digest.update(block)
    if count != expected_bytes or digest.hexdigest() != expected_sha:
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_HASH')
    active()
    kind = verify_magic(path)
    verify_suffix(kind, path.suffix.lower())
    if kind not in ('tiff', 'jpeg', 'png') or media_type != 'image/' + kind:
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_MEDIA')
    metadata = read_metadata(VerifiedSource(path, kind, expected_bytes, expected_sha))
    verify_raster_decode(path, check_active=active)
    active()
    after = path.lstat()
    if not stat.S_ISREG(after.st_mode) or pin != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns):
        raise ValueError('INTERCHANGE_EXPORT_IMAGE_CHANGED')
    return metadata
