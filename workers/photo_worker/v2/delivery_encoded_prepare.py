"""Version-pinned, bounded physical delivery encode evidence outside SQL TXs."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from tempfile import TemporaryDirectory

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export import EncodeParams, EncodedImage
from photo_worker.contracts.images import WorkingImage
from .adoption import AdoptionInvalid, ArtifactRef
from .delivery_encoded_verify import VerifiedEncodedRaster, verify_delivery_encoded_file


@dataclass(frozen=True)
class PreparedDeliveryEncode:
    params_jcs: bytes
    source_jcs: bytes
    manifest_jcs: bytes
    manifest_ref: ArtifactRef
    raster_ref: ArtifactRef
    raster: VerifiedEncodedRaster


def prepare_delivery_encode(reader, params_ref, root, *, scratch_root: Path,
        max_bytes: int, max_pixels: int, check_active=lambda: None):
    """Reader must be bound to current task/attempt/fence by SQL evidence.

    This prepares evidence only. Final SQL commit must recheck current authority,
    producer result/fence, Params and output refs; this function cannot adopt.
    """
    check_active()
    if (not isinstance(scratch_root, Path) or scratch_root.is_symlink()
            or not scratch_root.is_dir() or type(max_bytes) is not int
            or not 1 <= max_bytes <= 2**53-1 or type(max_pixels) is not int
            or not 1 <= max_pixels <= 200000000):
        raise AdoptionInvalid('DELIVERY_ENCODE_INSTALLATION_INVALID')
    params_ref = ArtifactRef(params_ref.artifact_id, params_ref.sha256)
    root = ArtifactRef(root.artifact_id, root.sha256)
    snapshots = {}

    def document(ref, model, source, role=None):
        check_active()
        evidence = reader.evidence(ref)
        if (evidence.ref != ref or evidence.source != source
                or role is not None and evidence.role != role
                or evidence.media_type != 'application/json'
                or type(evidence.bytes) is not int or not 2 <= evidence.bytes <= 1048576):
            raise AdoptionInvalid('DELIVERY_ENCODE_DOCUMENT_EVIDENCE_INVALID')
        raw = reader.read_json(ref, 1048576)
        if (not isinstance(raw, bytes) or len(raw) != evidence.bytes
                or sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('DELIVERY_ENCODE_DOCUMENT_BYTES_INVALID')
        value = model.model_validate_json(raw)
        if canonical_bytes(value) != raw:
            raise AdoptionInvalid('DELIVERY_ENCODE_DOCUMENT_NONCANONICAL')
        snapshots[ref] = evidence
        check_active()
        return value, raw

    try:
        params, params_raw = document(params_ref, EncodeParams, 'input')
        source_ref = ArtifactRef(params.image.artifact_id, params.image.sha256)
        source, source_raw = document(source_ref, WorkingImage, 'input')
        manifest, manifest_raw = document(root, EncodedImage, 'output', 'encoded_manifest')
        raster_ref = ArtifactRef(manifest.artifact.artifact_id, manifest.artifact.sha256)
        if len({params_ref.artifact_id, source_ref.artifact_id, root.artifact_id, raster_ref.artifact_id}) != 4:
            raise AdoptionInvalid('DELIVERY_ENCODE_OUTPUT_ALIAS')
        if reader.staged_outputs() != {root, raster_ref}:
            raise AdoptionInvalid('DELIVERY_ENCODE_OUTPUT_CLOSURE_INVALID')
        evidence = reader.evidence(raster_ref)
        media = {'jpeg': 'image/jpeg', 'png': 'image/png', 'tiff': 'image/tiff'}[params.format]
        if (evidence.ref != raster_ref or evidence.source != 'output' or evidence.role != 'encoded_image'
                or evidence.media_type != media or type(evidence.bytes) is not int
                or not 1 <= evidence.bytes <= max_bytes):
            raise AdoptionInvalid('DELIVERY_ENCODE_RASTER_EVIDENCE_INVALID')
        snapshots[raster_ref] = evidence
        with TemporaryDirectory(prefix='delivery-encode-', dir=scratch_root) as directory:
            path = Path(directory) / 'encoded.bin'
            count = 0
            digest = sha256()
            iterator = iter(reader.iter_bytes(raster_ref, evidence.bytes))
            try:
                with path.open('xb') as target:
                    for block in iterator:
                        check_active()
                        if (not isinstance(block, bytes) or not 1 <= len(block) <= 65536
                                or count + len(block) > evidence.bytes):
                            raise AdoptionInvalid('DELIVERY_ENCODE_STREAM_INVALID')
                        target.write(block)
                        digest.update(block)
                        count += len(block)
            finally:
                close = getattr(iterator, 'close', None)
                if close is not None:
                    close()
            if count != evidence.bytes or digest.hexdigest() != raster_ref.sha256:
                raise AdoptionInvalid('DELIVERY_ENCODE_STREAM_BYTES_INVALID')
            raster = verify_delivery_encoded_file(path, params, source, manifest,
                max_bytes=max_bytes, max_pixels=max_pixels, check_active=check_active)
            if raster.bytes != evidence.bytes:
                raise AdoptionInvalid('DELIVERY_ENCODE_RASTER_BYTES_CHANGED')
        for ref, original in snapshots.items():
            check_active()
            if reader.evidence(ref) != original:
                raise AdoptionInvalid('DELIVERY_ENCODE_EVIDENCE_CHANGED')
        if reader.staged_outputs() != {root, raster_ref}:
            raise AdoptionInvalid('DELIVERY_ENCODE_OUTPUT_CLOSURE_CHANGED')
        check_active()
        return PreparedDeliveryEncode(params_raw, source_raw, manifest_raw, root, raster_ref, raster)
    except AdoptionInvalid:
        raise
    except ValueError as error:
        raise AdoptionInvalid('DELIVERY_ENCODE_PHYSICAL_INVALID') from error
