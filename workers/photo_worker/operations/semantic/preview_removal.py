"""Private removal proposal compute; publication and Apply remain separate."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
from .preview_inpaint_compute import InpaintPreviewComputed as RemovalPreviewComputed, compute_inpaint_preview
from .preview_removal_loader import load_removal_preview_arrays


def compute_removal_preview(arrays, params, engine, *, verify_admission,
        max_bytes, check_active, strength):
    return compute_inpaint_preview(arrays, params, engine,
        verify_admission=verify_admission, max_bytes=max_bytes,
        check_active=check_active, strength=strength,
        input_model=SemanticRemovalPreviewInput)


def execute_artifact_removal_preview(artifacts, params, engine, *,
        verify_admission, verify_origin, verify_color, max_bytes, check_active):
    owned = SemanticRemovalPreviewInput.model_validate_json(canonical_bytes(params))
    if not all(callable(value) for value in (verify_admission,verify_origin,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    check_active()
    if verify_admission(owned.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
    check_active()
    from photo_worker.contracts.semantic import TransferCandidate, EditIntent
    from .preview_binding import bind_removal_preview_documents
    _, _, intent = bind_removal_preview_documents(owned,
        artifacts.contract_bytes(owned.candidate, TransferCandidate),
        artifacts.contract_bytes(owned.intent, EditIntent))
    arrays = load_removal_preview_arrays(artifacts, owned,
        verify_admission=verify_admission, verify_origin=verify_origin,
        verify_color=verify_color, max_bytes=max_bytes, check_active=check_active)
    return compute_removal_preview(arrays, owned, engine,
        verify_admission=verify_admission, max_bytes=max_bytes, check_active=check_active,
        strength=intent.strength)
