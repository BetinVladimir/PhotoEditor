"""Attempt-scoped background cleanup pixels; canonical publication is a separate step."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
from .preview_binding import bind_background_cleanup_preview_source
from .exposure_stage import stage_exposure_working_image


def stage_background_cleanup_preview(artifacts, params, source_raw, encoded, *,
        verify_admission, check_active, max_bytes, max_output_bytes):
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned = SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(params))
    image = bind_background_cleanup_preview_source(owned, source_raw)
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    admitted()
    # Shared writer changes only pixels; alpha, color, canvas and transform
    # linkage remain identical to the byte-bound source document.
    staged = stage_exposure_working_image(artifacts, image, encoded,
        admitted=admitted, check_active=check_active, max_bytes=max_bytes,
        max_output_bytes=max_output_bytes)
    inputs = {r.artifact_id for r in (owned.plan, owned.intent, owned.candidate,
        owned.source.source, owned.protection, image.pixels)}
    if image.alpha is not None:
        inputs.add(image.alpha.artifact_id)
    if staged.pixels.artifact_id in inputs or staged.image.artifact_id in inputs:
        raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
    admitted()
    return staged


def execute_and_stage_background_cleanup_preview(artifacts, params, engine, *,
        verify_admission, verify_origin, verify_color, check_active,
        max_bytes, max_output_bytes):
    from hashlib import sha256
    from .preview_background_cleanup import execute_artifact_background_cleanup_preview
    from .preview_stage import StagedExposurePreview
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.semantic_preview import BackgroundCleanupPreviewNumericalEvidence
    owned = SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(params))
    if type(max_output_bytes) is not int or not 1048576 <= max_output_bytes <= 2**53-1:
        raise ValueError('SEMANTIC_REMOVAL_RESOURCE_PROFILE')
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    admitted()
    computed = execute_artifact_background_cleanup_preview(artifacts, owned, engine,
        verify_admission=verify_admission, verify_origin=verify_origin,
        verify_color=verify_color, max_bytes=max_bytes, check_active=check_active)
    admitted()
    source_raw = artifacts.contract_bytes(owned.source.source, WorkingImage)
    admitted()
    staged = stage_background_cleanup_preview(artifacts, owned, source_raw, computed.candidate,
        verify_admission=verify_admission, check_active=check_active,
        max_bytes=max_bytes-computed.original.nbytes-computed.candidate.nbytes,
        max_output_bytes=max_output_bytes-1048576)
    evidence = BackgroundCleanupPreviewNumericalEvidence(schema_version=1, params=owned,
        working_image=staged.image, pixels=staged.pixels,
        algorithm='big_lama_work_srgb_delta_v1',
        changed_pixels=computed.preservation.changed_pixels,
        protected_pixels_unchanged=computed.preservation.protected_unchanged,
        outside_mask_unchanged=computed.preservation.outside_mask_unchanged,
        global_pass=computed.global_pass, roi=list(computed.roi))
    raw = canonical_bytes(evidence)
    if len(raw) > 1048576 or staged.bytes_written+len(raw) > max_output_bytes:
        raise ValueError('SEMANTIC_REMOVAL_RESOURCE_PROFILE')
    admitted()
    ref = ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(evidence, 'preview_evidence')))
    forbidden = {r.artifact_id for r in (owned.plan, owned.intent, owned.candidate,
        owned.source.source, owned.protection, staged.image, staged.pixels)}
    if (ref.artifact_id.int == 0 or ref.artifact_id in forbidden
            or ref.sha256 != sha256(raw).hexdigest() or canonical_bytes(evidence) != raw):
        raise ValueError('SEMANTIC_REMOVAL_OUTPUT_INVALID')
    admitted()
    return StagedExposurePreview(staged.pixels, staged.image, ref, staged.bytes_written+len(raw))
