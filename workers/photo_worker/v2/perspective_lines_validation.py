"""Source-bound automatic line evidence verification, without adoption authority.

The reader must establish current tenant/task/result/attempt/fence and immutable
object versions before and after IO. This does not qualify a detector or a
homography, independently replay pixels, or authorize a version Apply.
"""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.perspective_line_detection import (
    PerspectiveLineDetectionParams, PerspectiveLineDetectionResult)
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid, ArtifactRef, require_adoption_active


@dataclass(frozen=True)
class VerifiedPerspectiveLines:
    base_bytes: bytes
    result_bytes: bytes
    lines_bytes: bytes | None
    lines_sha256: str | None


def verify_perspective_lines_result(reader, params_ref, result_ref, *, expected_base,
                                    working_icc_sha, check_active=lambda: None):
    if not callable(check_active):
        raise AdoptionInvalid('PERSPECTIVE_LINES_ACTIVITY_INVALID')
    supplied_check_active=check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    try:
        if not isinstance(expected_base, VersionBinding): raise ValueError('invalid base')
        base = VersionBinding.model_validate_json(canonical_bytes(expected_base))
        if (base.asset_version_id.int == 0 or base.source.artifact_id.int == 0 or
                not isinstance(working_icc_sha, str) or len(working_icc_sha) != 64 or
                any(c not in '0123456789abcdef' for c in working_icc_sha)):
            raise ValueError('invalid binding')
        source_ref = ArtifactRef(base.source.artifact_id, base.source.sha256)
        if (not all(isinstance(ref, ArtifactRef) for ref in (params_ref, result_ref)) or
                len({ref.artifact_id for ref in (params_ref, result_ref, source_ref)}) != 3):
            raise ValueError('alias')
    except ValueError as exc:
        raise AdoptionInvalid('PERSPECTIVE_LINES_BINDING_INVALID') from exc

    def document(ref, model, source, role, limit):
        check_active()
        meta = reader.evidence(ref)
        check_active()
        if (meta.ref != ref or meta.source != source or
                (role is not None and meta.role not in (role if isinstance(role, tuple) else (role,))) or
                meta.media_type != 'application/json' or
                type(meta.bytes) is not int or not 2 <= meta.bytes <= limit):
            raise AdoptionInvalid('PERSPECTIVE_LINES_EVIDENCE_INVALID')
        raw = reader.read_json(ref, limit)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) != meta.bytes or
                sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('PERSPECTIVE_LINES_BYTES_INVALID')
        try:
            parse_json(raw, limit)
            value = model.model_validate_json(raw)
            if canonical_bytes(value) != raw: raise ValueError('noncanonical')
        except ValueError as exc:
            raise AdoptionInvalid('PERSPECTIVE_LINES_CONTRACT_INVALID') from exc
        return value, raw

    params, _ = document(params_ref, PerspectiveLineDetectionParams, 'input', (None, 'task_params'), 65536)
    result, raw = document(result_ref, PerspectiveLineDetectionResult, 'output', 'document', 1048576)
    image, _ = document(source_ref, WorkingImage, 'input', None, 1048576)
    if (params.base != base or result.base != base or image.dtype != 'float32' or
            image.color.icc_sha256 != working_icc_sha or
            canonical_hash(image.canvas) != base.canvas_hash or
            min(image.canvas.width, image.canvas.height) < 2 or
            image.canvas.width * image.canvas.height > 200000000):
        raise AdoptionInvalid('PERSPECTIVE_LINES_BINDING_INVALID')
    refs = [params_ref, result_ref, source_ref,
            ArtifactRef(image.pixels.artifact_id, image.pixels.sha256)]
    if image.alpha is not None:
        refs.append(ArtifactRef(image.alpha.artifact_id, image.alpha.sha256))
    if len({ref.artifact_id for ref in refs}) != len(refs):
        raise AdoptionInvalid('PERSPECTIVE_LINES_BINDING_INVALID')
    check_active()
    outputs = reader.staged_outputs()
    check_active()
    if outputs != {result_ref}:
        raise AdoptionInvalid('PERSPECTIVE_LINES_OUTPUTS_INVALID')
    lines = result.lines
    if lines is not None:
        # Match producer's bounded default output, with separated evidence in
        # both families. This does not establish straightness or image content.
        if len(lines.lines) > 128:
            raise AdoptionInvalid('PERSPECTIVE_LINES_CANDIDATES_INVALID')
        for family, axis in [('horizontal', 'y'), ('vertical', 'x')]:
            check_active()
            centers = [(getattr(line.a, axis) + getattr(line.b, axis)) / 2
                       for line in lines.lines if line.family == family]
            if len(centers) < 2 or max(centers) - min(centers) < .1:
                raise AdoptionInvalid('PERSPECTIVE_LINES_CANDIDATES_INVALID')
    body = None if lines is None else canonical_bytes(lines)
    check_active()
    return VerifiedPerspectiveLines(canonical_bytes(base), raw, body,
                                    None if body is None else sha256(body).hexdigest())
