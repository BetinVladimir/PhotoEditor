"""Complete attempt staging. Installation and canonical publication remain external."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import TransferCandidate, EditIntent
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput, ExposurePreviewNumericalEvidence
from .preview_binding import bind_exposure_preview_source
from .preview_loader import execute_artifact_exposure_preview, load_exposure_preview_arrays, load_exposure_preview_alpha
from .preview_stage import stage_exposure_preview
from .preview_media_prepare import prepare_exposure_preview_media
from .preview_media_stage import stage_exposure_preview_media
from .preview_presentation_stage import stage_preview_presentation


@dataclass(frozen=True)
class CompleteExposurePreview:
    presentation: ArtifactRef
    evidence: ArtifactRef
    bytes_written: int


def execute_complete_exposure_preview(artifacts, params, *, icc_root, icc_lock_sha,
        srgb_sha, max_bytes, max_output_bytes, verify_admission, verify_origin,
        verify_color, verify_runtime, check_active):
    if not all(callable(fn) for fn in (verify_admission, verify_origin, verify_color,
            verify_runtime, check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned = SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_ADMISSION_DENIED')
        check_active()
        return True
    admitted()
    source_raw = artifacts.contract_bytes(owned.source.source, WorkingImage)
    image = bind_exposure_preview_source(owned, source_raw)
    pixels = image.canvas.width * image.canvas.height
    display_pixels = min(pixels, 2560*2560)
    if (type(max_bytes) is not int or not 640*pixels+128*1024**2 <= max_bytes <= 2**53-1
            or type(max_output_bytes) is not int
            or not 12*pixels+16384+20*display_pixels+4*65536+8*1048576 <= max_output_bytes <= 2**53-1):
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    common = dict(verify_admission=verify_admission, verify_origin=verify_origin,
        verify_color=verify_color, check_active=check_active)
    encoded = execute_artifact_exposure_preview(artifacts, owned, max_bytes=max_bytes, **common)
    # Reload through the same provenance-checked loader; account for resident output.
    arrays = load_exposure_preview_arrays(artifacts, owned, max_bytes=max_bytes-12*pixels, **common)
    alpha = load_exposure_preview_alpha(artifacts, owned, source_raw,
        verify_admission=verify_admission, max_bytes=max_bytes-32*pixels, check_active=check_active)
    candidate_raw = artifacts.contract_bytes(owned.candidate, TransferCandidate)
    intent_raw = artifacts.contract_bytes(owned.intent, EditIntent)
    media = prepare_exposure_preview_media(owned, candidate_raw, intent_raw, source_raw,
        arrays, encoded, alpha, icc_root=icc_root, icc_lock_sha=icc_lock_sha, srgb_sha=srgb_sha,
        max_bytes=max_bytes, verify_admission=verify_admission, verify_runtime=verify_runtime,
        verify_color=verify_color, check_active=check_active)
    del arrays, alpha
    admitted()
    staged = stage_exposure_preview(artifacts, owned, source_raw, encoded,
        verify_admission=verify_admission, check_active=check_active,
        max_bytes=max_bytes-64*display_pixels-12*pixels, max_output_bytes=max_output_bytes)
    del encoded
    evidence = ExposurePreviewNumericalEvidence(schema_version=1, params=owned,
        working_image=staged.image, pixels=staged.pixels,
        algorithm='local_exposure_exact_replay_v1', protected_pixels_unchanged=True,
        outside_mask_max_error=0.)
    raw = canonical_bytes(evidence)
    admitted()
    ref = ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(evidence, 'preview_evidence')))
    forbidden = {r.artifact_id for r in (owned.plan, owned.intent, owned.candidate,
        owned.source.source, owned.protection, staged.image, staged.pixels)}
    if (ref.artifact_id.int == 0 or ref.artifact_id in forbidden
            or ref.sha256 != sha256(raw).hexdigest() or canonical_bytes(evidence) != raw):
        raise ValueError('SEMANTIC_EXPOSURE_OUTPUT_INVALID')
    forbidden.add(ref.artifact_id)
    used = staged.bytes_written + len(raw)
    admitted()
    outputs = stage_exposure_preview_media(artifacts, before=media.before, after=media.after,
        affected=media.affected, protected=media.protected, forbidden_ids=forbidden,
        srgb_sha=srgb_sha, max_bytes=max_bytes, max_output_bytes=max_output_bytes-used,
        verify_runtime=verify_runtime, verify_admission=admitted, check_active=check_active)
    presentation, size = stage_preview_presentation(artifacts, evidence_ref=ref, evidence_raw=raw,
        media=outputs, width=media.width, height=media.height, icc_lock_sha=icc_lock_sha,
        srgb_sha=srgb_sha, max_output_bytes=max_output_bytes-used,
        verify_admission=admitted, check_active=check_active)
    admitted()
    return CompleteExposurePreview(presentation, ref, used+size)
