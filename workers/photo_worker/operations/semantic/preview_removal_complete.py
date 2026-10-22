"""Complete attempt staging. Installation and canonical publication remain external."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import TransferCandidate, EditIntent
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput, RemovalPreviewNumericalEvidence
from .preview_binding import bind_removal_preview_source
from .preview_removal import execute_artifact_removal_preview
from .preview_removal_loader import load_removal_preview_arrays
from .preview_loader import load_removal_preview_alpha
from .preview_removal_stage import stage_removal_preview
from .preview_removal_media import prepare_removal_preview_media
from .preview_media_stage import stage_exposure_preview_media
from .preview_presentation_stage import stage_removal_preview_presentation


@dataclass(frozen=True)
class CompleteRemovalPreview:
    presentation: ArtifactRef
    evidence: ArtifactRef
    bytes_written: int


def execute_complete_removal_preview(artifacts, params, engine, *, icc_root, icc_lock_sha,
        srgb_sha, max_bytes, max_output_bytes, verify_admission, verify_origin,
        verify_color, verify_runtime, check_active):
    if not all(callable(fn) for fn in (verify_admission, verify_origin, verify_color,
            verify_runtime, check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned = SemanticRemovalPreviewInput.model_validate_json(canonical_bytes(params))
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_ADMISSION_DENIED')
        check_active()
        return True
    def document(ref, model):
        admitted()
        raw = artifacts.contract_bytes(ref.model_copy(deep=True), model)
        admitted()
        return raw
    source_raw = document(owned.source.source, WorkingImage)
    image = bind_removal_preview_source(owned, source_raw)
    pixels = image.canvas.width * image.canvas.height
    display_pixels = min(pixels, 2560*2560)
    if (type(max_bytes) is not int or not 768*pixels+512*1024**2 <= max_bytes <= 2**53-1
            or type(max_output_bytes) is not int
            or not 12*pixels+16384+20*display_pixels+4*65536+8*1048576 <= max_output_bytes <= 2**53-1):
        raise ValueError('SEMANTIC_REMOVAL_RESOURCE_PROFILE')
    from .preview_removal_inputs import removal_declared_inputs
    from .preview_removal_output_closure import RemovalOutputClosure, RecordingRemovalArtifacts
    declared = list(removal_declared_inputs(artifacts, check_active=admitted))
    ledger = RemovalOutputClosure(declared, max_output_bytes=max_output_bytes)
    required = [owned.plan, owned.intent, owned.candidate, owned.source.source,
        owned.protection, image.pixels]
    if image.alpha is not None:
        required.append(image.alpha)
    if any(ledger._inputs.get(ref.artifact_id) != ref.sha256 for ref in required):
        raise ValueError('SEMANTIC_REMOVAL_OUTPUT_CLOSURE_INVALID')
    artifacts = RecordingRemovalArtifacts(artifacts, ledger, check_active=admitted)
    common = dict(verify_admission=verify_admission, verify_origin=verify_origin,
        verify_color=verify_color, check_active=check_active)
    computed = execute_artifact_removal_preview(artifacts, owned, engine, max_bytes=max_bytes, **common)
    encoded = computed.candidate
    changed_pixels = computed.preservation.changed_pixels
    protected_unchanged = computed.preservation.protected_unchanged
    outside_unchanged = computed.preservation.outside_mask_unchanged
    roi = list(computed.roi)
    global_pass = computed.global_pass
    del computed
    # Reload through the same provenance-checked loader; account for resident output.
    arrays = load_removal_preview_arrays(artifacts, owned, max_bytes=max_bytes-12*pixels, **common)
    alpha = load_removal_preview_alpha(artifacts, owned, source_raw,
        verify_admission=verify_admission, max_bytes=max_bytes-32*pixels, check_active=check_active)
    candidate_raw = document(owned.candidate, TransferCandidate)
    intent_raw = document(owned.intent, EditIntent)
    media = prepare_removal_preview_media(owned, candidate_raw, intent_raw, source_raw,
        arrays, encoded, alpha, icc_root=icc_root, icc_lock_sha=icc_lock_sha, srgb_sha=srgb_sha,
        max_bytes=max_bytes, verify_admission=verify_admission, verify_runtime=verify_runtime,
        verify_color=verify_color, check_active=check_active)
    del arrays, alpha
    admitted()
    staged = stage_removal_preview(artifacts, owned, source_raw, encoded,
        verify_admission=verify_admission, check_active=check_active,
        max_bytes=max_bytes-64*display_pixels-12*pixels, max_output_bytes=max_output_bytes)
    del encoded
    evidence = RemovalPreviewNumericalEvidence(schema_version=1, params=owned,
        working_image=staged.image, pixels=staged.pixels,
        algorithm='big_lama_work_srgb_delta_v1', protected_pixels_unchanged=protected_unchanged,
        outside_mask_unchanged=outside_unchanged, changed_pixels=changed_pixels, roi=roi, global_pass=global_pass)
    raw = canonical_bytes(evidence)
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
    presentation, size = stage_removal_preview_presentation(artifacts, evidence_ref=ref, evidence_raw=raw,
        media=outputs, width=media.width, height=media.height, icc_lock_sha=icc_lock_sha,
        srgb_sha=srgb_sha, max_output_bytes=max_output_bytes-used,
        verify_admission=admitted, check_active=check_active)
    admitted()
    ledger.verify(owned, source_raw, presentation_ref=presentation, expected_bytes=used+size)
    admitted()
    return CompleteRemovalPreview(presentation, ref, used+size)
