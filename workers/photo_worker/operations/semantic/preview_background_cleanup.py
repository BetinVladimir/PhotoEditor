"""Private background-cleanup proposal compute; not an installed worker route."""
from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
from .preview_inpaint_compute import InpaintPreviewComputed as BackgroundCleanupPreviewComputed, compute_inpaint_preview


def compute_background_cleanup_preview(arrays, params, engine, *, verify_admission,
        max_bytes, check_active, strength):
    return compute_inpaint_preview(arrays, params, engine,
        verify_admission=verify_admission, max_bytes=max_bytes,
        check_active=check_active, strength=strength,
        input_model=SemanticBackgroundCleanupPreviewInput)


def execute_artifact_background_cleanup_preview(artifacts, params, engine, *,
        verify_admission, verify_origin, verify_color, max_bytes, check_active):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.semantic import TransferCandidate, EditIntent
    from .preview_binding import bind_background_cleanup_preview_documents
    from .preview_background_cleanup_loader import load_background_cleanup_preview_arrays
    owned = SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(params))
    if not all(callable(value) for value in (verify_admission,verify_origin,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    def read_contract(ref, model):
        admitted()
        raw = artifacts.contract_bytes(ref, model)
        admitted()
        return raw
    _, _, intent = bind_background_cleanup_preview_documents(owned,
        read_contract(owned.candidate, TransferCandidate),
        read_contract(owned.intent, EditIntent))
    arrays = load_background_cleanup_preview_arrays(artifacts, owned,
        verify_admission=verify_admission, verify_origin=verify_origin,
        verify_color=verify_color, max_bytes=max_bytes, check_active=check_active)
    return compute_background_cleanup_preview(arrays, owned, engine,
        verify_admission=verify_admission, max_bytes=max_bytes,
        check_active=check_active, strength=intent.strength)
