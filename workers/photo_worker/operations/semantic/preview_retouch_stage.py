"""Attempt-scoped retouch image staging; no canonical adoption or receipt."""
from contextlib import contextmanager
from dataclasses import dataclass
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic_preview import RetouchPreviewNumericalEvidence
from .candidate_preservation import CandidatePreservation
from .preview_retouch_compute import RetouchPreviewComputed
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from .preview_binding import bind_retouch_preview_source
from .exposure_stage import stage_exposure_working_image


def stage_retouch_preview(artifacts, params, source_raw, encoded, *,
        verify_admission, check_active, max_bytes, max_output_bytes):
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_RETOUCH_INSTALLATION_REQUIRED')
    owned = SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    image = bind_retouch_preview_source(owned, source_raw)

    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()

    admitted()
    # Exact existing writer preserves source alpha/ICC/canvas/transform and
    # writes float32 pixels; no additional tone or beauty pass is performed.
    staged = stage_exposure_working_image(artifacts, image, encoded,
        admitted=admitted, check_active=check_active, max_bytes=max_bytes,
        max_output_bytes=max_output_bytes)
    forbidden = {ref.artifact_id for ref in (owned.plan, owned.intent, owned.candidate,
        owned.source.source, owned.protection, image.pixels)}
    if image.alpha is not None:
        forbidden.add(image.alpha.artifact_id)
    if staged.image.artifact_id in forbidden or staged.pixels.artifact_id in forbidden:
        raise ValueError('SEMANTIC_RETOUCH_ARTIFACT_ALIAS')
    if staged.bytes_written > max_output_bytes:
        raise ValueError('SEMANTIC_RETOUCH_RESOURCE_PROFILE')
    admitted()
    return staged


@dataclass(frozen=True)
class StagedRetouchPreview:
    pixels: ArtifactRef
    image: ArtifactRef
    evidence: ArtifactRef
    bytes_written: int


@contextmanager
def execute_and_stage_retouch_preview(artifacts, params, execute, *,
        verify_admission, check_active, max_ram_bytes, max_output_bytes):
    """Stage within compute lifetime; caller must qualify and own execute()."""
    if not all(callable(fn) for fn in (execute, verify_admission, check_active)):
        raise ValueError('SEMANTIC_RETOUCH_INSTALLATION_REQUIRED')
    if (type(max_ram_bytes) is not int or not 1 <= max_ram_bytes <= 2**53-1
            or type(max_output_bytes) is not int or not 3*1048576 <= max_output_bytes <= 2**53-1):
        raise ValueError('SEMANTIC_RETOUCH_RESOURCE_PROFILE')
    owned = SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))

    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()

    admitted()
    source_raw = artifacts.contract_bytes(owned.source.source.model_copy(deep=True), WorkingImage)
    admitted()
    source = bind_retouch_preview_source(owned, source_raw)
    width, height = source.canvas.width, source.canvas.height
    with execute() as computed:
        admitted()
        if (type(computed) is not RetouchPreviewComputed
                or type(computed.preservation) is not CandidatePreservation
                or not isinstance(computed.pixels, np.ndarray)
                or computed.pixels.shape != (height, width, 3)
                or computed.pixels.dtype != np.float32
                or type(computed.peak_bytes) is not int
                or not 1 <= computed.peak_bytes < max_ram_bytes):
            raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
        proof = computed.preservation
        if (type(proof.pixels) is not int or proof.pixels != width*height
                or type(proof.changed_pixels) is not int or not 0 <= proof.changed_pixels <= proof.pixels
                or any(value is not True for value in (proof.outside_mask_unchanged,
                    proof.protected_unchanged, proof.alpha_unchanged))):
            raise ValueError('SEMANTIC_RETOUCH_PRESERVATION_INVALID')
        staged = stage_retouch_preview(artifacts, owned, source_raw, computed.pixels,
            verify_admission=verify_admission, check_active=check_active,
            max_bytes=max_ram_bytes-computed.peak_bytes,
            max_output_bytes=max_output_bytes-1048576)
        evidence = RetouchPreviewNumericalEvidence(schema_version=1, params=owned,
            working_image=staged.image, pixels=staged.pixels, algorithm='beautyfilter_v1',
            changed_pixels=proof.changed_pixels,
            protected_pixels_unchanged=proof.protected_unchanged,
            outside_mask_unchanged=proof.outside_mask_unchanged,
            alpha_unchanged=proof.alpha_unchanged, roi=[0, 0, width, height])
        raw = canonical_bytes(evidence)
        if len(raw) > 1048576 or staged.bytes_written+len(raw) > max_output_bytes:
            raise ValueError('SEMANTIC_RETOUCH_RESOURCE_PROFILE')
        admitted()
        ref = ArtifactRef.model_validate_json(canonical_bytes(
            artifacts.stage_contract(evidence, 'preview_evidence')))
        forbidden = {r.artifact_id for r in (owned.plan, owned.intent, owned.candidate,
            owned.source.source, owned.protection, source.pixels, staged.image, staged.pixels)}
        if source.alpha is not None:
            forbidden.add(source.alpha.artifact_id)
        if (ref.artifact_id.int == 0 or ref.artifact_id in forbidden
                or ref.sha256 != sha256(raw).hexdigest() or canonical_bytes(evidence) != raw):
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_INVALID')
        admitted()
        yield StagedRetouchPreview(staged.pixels, staged.image, ref, staged.bytes_written+len(raw))
        admitted()
    admitted()
