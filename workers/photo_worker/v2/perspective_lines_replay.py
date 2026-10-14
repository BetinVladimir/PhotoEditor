"""Pinned independent reconstruction of automatic-line output from source NPY.

The caller must establish current task/attempt/fence/object-version authority
before and after IO; this verifier does not persist adoption or permit Apply.
"""
from pathlib import Path
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.optical.perspective_lines_runtime_lock import PerspectiveLinesRuntimeLock, verify_installed_runtime
from photo_worker.operations.optical.perspective_line_evidence import prepare_detected_perspective_lines
from photo_worker.operations.optical.perspective_line_resources import line_detection_resources
from .adoption import AdoptionInvalid, ArtifactRef, require_adoption_active
from .array_evidence import ArrayEvidenceSession
from .perspective_lines_validation import verify_perspective_lines_result


def verify_pinned_perspective_lines_replay(reader, params_ref, result_ref, *, expected_base,
        working_icc_sha, runtime_lock, runtime_sha, scratch_root, max_bytes, ram_max_bytes, check_active):
    if (not callable(check_active) or not isinstance(scratch_root, Path)
            or any(type(value) is not int or not 1 <= value <= 2**53-1 for value in (max_bytes, ram_max_bytes))):
        raise AdoptionInvalid('PERSPECTIVE_LINES_REPLAY_CONFIG_INVALID')
    supplied_check_active=check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    lock = PerspectiveLinesRuntimeLock.model_validate_json(canonical_bytes(runtime_lock))
    verify_installed_runtime(lock, runtime_sha)
    verified = verify_perspective_lines_result(reader, params_ref, result_ref,
        expected_base=expected_base, working_icc_sha=working_icc_sha, check_active=check_active)
    source_ref = ArtifactRef(expected_base.source.artifact_id, expected_base.source.sha256)
    raw = reader.read_json(source_ref, 1048576)
    source = WorkingImage.model_validate_json(raw)
    if (not isinstance(raw, bytes) or sha256(raw).hexdigest() != source_ref.sha256
            or canonical_bytes(source) != raw or source.dtype != 'float32'
            or source.color.icc_sha256 != working_icc_sha
            or canonical_hash(source.canvas) != expected_base.canvas_hash):
        raise AdoptionInvalid('PERSPECTIVE_LINES_REPLAY_SOURCE_INVALID')
    required = line_detection_resources(source.canvas.width, source.canvas.height, source.alpha is not None)
    if max_bytes < required['scratch_bytes'] or ram_max_bytes < required['ram_bytes']:
        raise AdoptionInvalid('PERSPECTIVE_LINES_REPLAY_RESOURCE_EXCEEDED')
    shape = (source.canvas.height, source.canvas.width)
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, check_active) as arrays:
        rgb = arrays.array(ArtifactRef(source.pixels.artifact_id, source.pixels.sha256), (*shape, 3),
            source='input', legacy_input_npy=True)
        alpha = None
        if source.alpha is not None:
            alpha = arrays.array(ArtifactRef(source.alpha.artifact_id, source.alpha.sha256), shape,
                source='input', unit=True, legacy_alpha=True, legacy_input_npy=True)
        try:
            replay = prepare_detected_perspective_lines(raw, source_ref.sha256, rgb, alpha,
                working_icc_sha=working_icc_sha, check_active=check_active)
        except ValueError as error:
            raise AdoptionInvalid('PERSPECTIVE_LINES_REPLAY_SOURCE_INVALID') from error
        if replay.lines != verified.lines_bytes or replay.lines_sha256 != verified.lines_sha256:
            raise AdoptionInvalid('PERSPECTIVE_LINES_PIXEL_REPLAY_MISMATCH')
    check_active()
    verify_installed_runtime(lock, runtime_sha)
    # Repeat original document/evidence validation after all source IO.
    repeated = verify_perspective_lines_result(reader, params_ref, result_ref,
        expected_base=expected_base, working_icc_sha=working_icc_sha, check_active=check_active)
    if repeated != verified:
        raise AdoptionInvalid('PERSPECTIVE_LINES_REPLAY_CHANGED')
    check_active()
    return verified
