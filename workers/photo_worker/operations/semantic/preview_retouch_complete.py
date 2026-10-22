"""Own complete attempt presentation; canonical publication remains external."""
from contextlib import contextmanager
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import TransferCandidate, EditIntent
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput, RetouchPreviewNumericalEvidence
from .preview_binding import bind_retouch_preview_source
from .preview_retouch_stage import execute_and_stage_retouch_preview
from .preview_retouch_loader import load_retouch_preview_arrays, load_retouch_preview_alpha
from .preview_retouch_media import prepare_retouch_preview_media
from .preview_media_stage import stage_exposure_preview_media
from .preview_presentation_stage import stage_retouch_preview_presentation


@dataclass(frozen=True)
class CompleteRetouchPreview:
    presentation: ArtifactRef
    evidence: ArtifactRef
    bytes_written: int


def validate_retouch_preview_budget(image, max_ram_bytes, max_output_bytes):
    pixels = image.canvas.width * image.canvas.height
    display = min(pixels, 2560*2560)
    if (type(max_ram_bytes) is not int
            or not 960*pixels+768*1048576+256*4194304 <= max_ram_bytes <= 2**53-1
            or type(max_output_bytes) is not int
            or not 12*pixels+16384+20*display+4*65536+8*1048576 <= max_output_bytes <= 2**53-1):
        raise ValueError('SEMANTIC_RETOUCH_RESOURCE_PROFILE')
    return pixels


def _execute_complete_retouch_preview(artifacts, params, execute, *, icc_root, icc_lock_sha,
        srgb_sha, max_ram_bytes, max_output_bytes, verify_admission, verify_origin,
        verify_color, verify_runtime, check_active):
    if not all(callable(fn) for fn in (execute, verify_admission, verify_origin,
            verify_color, verify_runtime, check_active)):
        raise ValueError('SEMANTIC_RETOUCH_INSTALLATION_REQUIRED')
    owned = SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    def admitted(value=None):
        check_active()
        if value is not None and canonical_bytes(value) != canonical_bytes(owned):
            return False
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_ADMISSION_DENIED')
        check_active()
        return True
    def read(ref, model):
        admitted()
        raw = artifacts.contract_bytes(ref.model_copy(deep=True), model)
        admitted()
        return raw
    source_raw = read(owned.source.source, WorkingImage)
    image = bind_retouch_preview_source(owned, source_raw)
    validate_retouch_preview_budget(image, max_ram_bytes, max_output_bytes)
    current = None
    @contextmanager
    def capture():
        nonlocal current
        with execute() as computed:
            current = computed
            try:
                yield computed
            finally:
                current = None
    with execute_and_stage_retouch_preview(artifacts, owned, capture,
            verify_admission=admitted, check_active=check_active,
            max_ram_bytes=max_ram_bytes, max_output_bytes=max_output_bytes) as staged:
        admitted()
        available = max_ram_bytes-current.peak_bytes
        arrays = load_retouch_preview_arrays(artifacts, owned, verify_admission=admitted,
            verify_origin=verify_origin, verify_color=verify_color,
            max_bytes=available, check_active=admitted)
        admitted()
        resident = arrays.original.nbytes+arrays.mask.nbytes+arrays.protected.nbytes
        alpha = load_retouch_preview_alpha(artifacts, owned, source_raw,
            verify_admission=admitted, max_bytes=available-resident, check_active=admitted)
        admitted()
        if alpha is not None:
            resident += alpha.nbytes
        candidate_raw = read(owned.candidate, TransferCandidate)
        intent_raw = read(owned.intent, EditIntent)
        media = prepare_retouch_preview_media(owned, candidate_raw, intent_raw, source_raw,
            arrays, current.pixels, alpha, icc_root=icc_root, icc_lock_sha=icc_lock_sha,
            srgb_sha=srgb_sha, max_bytes=available-resident,
            verify_admission=admitted, verify_runtime=verify_runtime,
            verify_color=verify_color, check_active=admitted)
        del arrays, alpha
        admitted()
        proof = current.preservation
        evidence = RetouchPreviewNumericalEvidence(schema_version=1, params=owned,
            working_image=staged.image, pixels=staged.pixels, algorithm='beautyfilter_v1',
            changed_pixels=proof.changed_pixels, protected_pixels_unchanged=proof.protected_unchanged,
            outside_mask_unchanged=proof.outside_mask_unchanged, alpha_unchanged=proof.alpha_unchanged,
            roi=[0,0,image.canvas.width,image.canvas.height])
        raw = canonical_bytes(evidence)
        if sha256(raw).hexdigest() != staged.evidence.sha256:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_INVALID')
        forbidden = {ref.artifact_id for ref in (owned.plan, owned.intent, owned.candidate,
            owned.source.source, owned.protection, image.pixels, staged.image, staged.pixels, staged.evidence)}
        if image.alpha is not None:
            forbidden.add(image.alpha.artifact_id)
        outputs = stage_exposure_preview_media(artifacts, before=media.before, after=media.after,
            affected=media.affected, protected=media.protected, forbidden_ids=forbidden,
            srgb_sha=srgb_sha, max_bytes=available, max_output_bytes=max_output_bytes-staged.bytes_written,
            verify_runtime=verify_runtime, verify_admission=admitted, check_active=admitted)
        admitted()
        presentation, size = stage_retouch_preview_presentation(artifacts,
            evidence_ref=staged.evidence, evidence_raw=raw, media=outputs,
            width=media.width, height=media.height, icc_lock_sha=icc_lock_sha, srgb_sha=srgb_sha,
            max_output_bytes=max_output_bytes-staged.bytes_written,
            verify_admission=admitted, check_active=admitted)
        admitted()
        result = CompleteRetouchPreview(presentation, staged.evidence, staged.bytes_written+size)
        del media
    # Context exit rechecks qualification/cancellation before returning success.
    admitted()
    return result


def execute_complete_retouch_preview(artifacts, params, execute, *, declared_inputs,
        icc_root, icc_lock_sha, srgb_sha, max_ram_bytes, max_output_bytes,
        verify_admission, verify_origin, verify_color, verify_runtime, check_active):
    from .preview_retouch_output_closure import RetouchOutputClosure, RecordingRetouchArtifacts
    if not all(callable(fn) for fn in (verify_admission, check_active)):
        raise ValueError('SEMANTIC_RETOUCH_INSTALLATION_REQUIRED')
    owned = SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    def active():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_ADMISSION_DENIED')
        check_active()
    active()
    ledger = RetouchOutputClosure(declared_inputs, max_output_bytes=max_output_bytes)
    for ref in (owned.plan, owned.intent, owned.candidate, owned.source.source, owned.protection):
        if ledger._inputs.get(ref.artifact_id) != ref.sha256:
            raise ValueError('SEMANTIC_RETOUCH_OUTPUT_CLOSURE_INVALID')
    recorded = RecordingRetouchArtifacts(artifacts, ledger, check_active=active)
    source_raw = recorded.contract_bytes(owned.source.source.model_copy(deep=True), WorkingImage)
    active()
    bind_retouch_preview_source(owned, source_raw)
    result = _execute_complete_retouch_preview(recorded, owned, execute,
        icc_root=icc_root, icc_lock_sha=icc_lock_sha, srgb_sha=srgb_sha,
        max_ram_bytes=max_ram_bytes, max_output_bytes=max_output_bytes,
        verify_admission=verify_admission, verify_origin=verify_origin,
        verify_color=verify_color, verify_runtime=verify_runtime, check_active=check_active)
    active()
    ledger.verify(owned, source_raw, presentation_ref=result.presentation, expected_bytes=result.bytes_written)
    active()
    return result
