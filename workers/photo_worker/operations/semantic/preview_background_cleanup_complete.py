"""Complete attempt staging. Installation and canonical publication remain external."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import TransferCandidate, EditIntent
from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput, BackgroundCleanupPreviewNumericalEvidence
from .preview_binding import bind_background_cleanup_preview_source
from .preview_background_cleanup import execute_artifact_background_cleanup_preview
from .preview_background_cleanup_loader import load_background_cleanup_preview_arrays
from .preview_loader import load_background_cleanup_preview_alpha
from .preview_background_cleanup_stage import stage_background_cleanup_preview
from .preview_background_cleanup_media import prepare_background_cleanup_preview_media
from .preview_media_stage import stage_exposure_preview_media
from .preview_presentation_stage import stage_background_cleanup_preview_presentation


@dataclass(frozen=True)
class CompleteBackgroundCleanupPreview:
    presentation: ArtifactRef
    evidence: ArtifactRef
    bytes_written: int


def validate_background_cleanup_preview_budget(image, max_bytes, max_output_bytes):
    """Shared host/output admission, checked before model acquisition and compute."""
    pixels = image.canvas.width * image.canvas.height
    display_pixels = min(pixels, 2560*2560)
    if (type(max_bytes) is not int or not 768*pixels+512*1024**2 <= max_bytes <= 2**53-1
            or type(max_output_bytes) is not int
            or not 12*pixels+16384+20*display_pixels+4*65536+8*1048576 <= max_output_bytes <= 2**53-1):
        raise ValueError('SEMANTIC_REMOVAL_RESOURCE_PROFILE')
    return pixels, display_pixels


def execute_complete_background_cleanup_preview(artifacts, params, engine, *, icc_root, icc_lock_sha,
        srgb_sha, max_bytes, max_output_bytes, verify_admission, verify_origin,
        verify_color, verify_runtime, check_active):
    if not all(callable(fn) for fn in (verify_admission, verify_origin, verify_color,
            verify_runtime, check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned = SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(params))
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_ADMISSION_DENIED')
        check_active()
        return True
    admitted()
    def read_contract(ref, model):
        # A read can outlive cancellation or installation/admission changes.
        # Fence both sides before permitting the next IO or computation.
        admitted()
        raw = artifacts.contract_bytes(ref, model)
        admitted()
        return raw
    source_raw = read_contract(owned.source.source, WorkingImage)
    image = bind_background_cleanup_preview_source(owned, source_raw)
    pixels, display_pixels = validate_background_cleanup_preview_budget(image, max_bytes, max_output_bytes)
    common = dict(verify_admission=verify_admission, verify_origin=verify_origin,
        verify_color=verify_color, check_active=check_active)
    computed = execute_artifact_background_cleanup_preview(artifacts, owned, engine, max_bytes=max_bytes, **common)
    admitted()
    encoded = computed.candidate
    changed_pixels = computed.preservation.changed_pixels
    roi = list(computed.roi)
    global_pass = computed.global_pass
    del computed
    # Reload through the same provenance-checked loader; account for resident output.
    arrays = load_background_cleanup_preview_arrays(artifacts, owned, max_bytes=max_bytes-12*pixels, **common)
    admitted()
    alpha = load_background_cleanup_preview_alpha(artifacts, owned, source_raw,
        verify_admission=verify_admission, max_bytes=max_bytes-32*pixels, check_active=check_active)
    admitted()
    candidate_raw = read_contract(owned.candidate, TransferCandidate)
    intent_raw = read_contract(owned.intent, EditIntent)
    media = prepare_background_cleanup_preview_media(owned, candidate_raw, intent_raw, source_raw,
        arrays, encoded, alpha, icc_root=icc_root, icc_lock_sha=icc_lock_sha, srgb_sha=srgb_sha,
        max_bytes=max_bytes, verify_admission=verify_admission, verify_runtime=verify_runtime,
        verify_color=verify_color, check_active=check_active)
    del arrays, alpha
    admitted()
    staged = stage_background_cleanup_preview(artifacts, owned, source_raw, encoded,
        verify_admission=verify_admission, check_active=check_active,
        max_bytes=max_bytes-64*display_pixels-12*pixels, max_output_bytes=max_output_bytes)
    del encoded
    evidence = BackgroundCleanupPreviewNumericalEvidence(schema_version=1, params=owned,
        working_image=staged.image, pixels=staged.pixels,
        algorithm='big_lama_work_srgb_delta_v1', protected_pixels_unchanged=True,
        outside_mask_unchanged=True, changed_pixels=changed_pixels, roi=roi, global_pass=global_pass)
    raw = canonical_bytes(evidence)
    if len(raw) > 1048576 or staged.bytes_written + len(raw) > max_output_bytes:
        raise ValueError('SEMANTIC_REMOVAL_RESOURCE_PROFILE')
    admitted()
    ref = ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(evidence, 'preview_evidence')))
    forbidden = {r.artifact_id for r in (owned.plan, owned.intent, owned.candidate,
        owned.source.source, owned.protection, staged.image, staged.pixels)}
    if (ref.artifact_id.int == 0 or ref.artifact_id in forbidden
            or ref.sha256 != sha256(raw).hexdigest() or canonical_bytes(evidence) != raw):
        raise ValueError('SEMANTIC_REMOVAL_OUTPUT_INVALID')
    forbidden.add(ref.artifact_id)
    used = staged.bytes_written + len(raw)
    admitted()
    outputs = stage_exposure_preview_media(artifacts, before=media.before, after=media.after,
        affected=media.affected, protected=media.protected, forbidden_ids=forbidden,
        srgb_sha=srgb_sha, max_bytes=max_bytes, max_output_bytes=max_output_bytes-used,
        verify_runtime=verify_runtime, verify_admission=admitted, check_active=check_active)
    presentation, size = stage_background_cleanup_preview_presentation(artifacts, evidence_ref=ref, evidence_raw=raw,
        media=outputs, width=media.width, height=media.height, icc_lock_sha=icc_lock_sha,
        srgb_sha=srgb_sha, max_output_bytes=max_output_bytes-used,
        verify_admission=admitted, check_active=check_active)
    admitted()
    return CompleteBackgroundCleanupPreview(presentation, ref, used+size)
