"""Receipt-admitted shown-pixel copy execution; no global operation registration."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit,TransferCandidate,EditIntent
from photo_worker.contracts.semantic_preview import (ShownBackgroundCleanupAdoptionInput,
    BackgroundCleanupPreviewPresentation,BackgroundCleanupPreviewNumericalEvidence)
from .preview_background_cleanup_adopt_binding import bind_shown_background_cleanup_adoption
from .preview_adopt_stage import stage_shown_preview_image


def execute_shown_background_cleanup_adoption(artifacts,params,*,verify_admission,verify_runtime,
        verify_color,check_active,max_output_bytes,expected_pixel_count):
    if not all(callable(fn) for fn in (verify_admission,verify_runtime,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=ShownBackgroundCleanupAdoptionInput.model_validate_json(canonical_bytes(params))
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_DENIED')
        check_active()
        return True
    admitted()
    def document(ref,model):
        admitted()
        raw=artifacts.contract_bytes(ref,model)
        admitted()
        return raw
    approved=document(owned.approved_edit,ApprovedSemanticEdit)
    presentation=document(owned.presentation,BackgroundCleanupPreviewPresentation)
    evidence=document(owned.numerical_evidence,BackgroundCleanupPreviewNumericalEvidence)
    image_raw=document(owned.working_image,WorkingImage)
    preview=BackgroundCleanupPreviewPresentation.model_validate_json(presentation).params
    # Every dynamically expanded reference must already be in the frozen lease
    # inventory; TaskArtifacts rejects arbitrary undisclosed artifact reads.
    candidate=document(preview.candidate,TransferCandidate)
    intent=document(preview.intent,EditIntent)
    source=document(preview.source.source,WorkingImage)
    _,_,_,image=bind_shown_background_cleanup_adoption(owned,approved,presentation,evidence,image_raw,
        candidate,intent,source)
    if type(expected_pixel_count) is not int or image.canvas.width*image.canvas.height!=expected_pixel_count:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    admitted()
    if verify_color(image.color.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')
    admitted()
    return stage_shown_preview_image(artifacts,owned.working_image,image_raw,
        verify_admission=admitted,check_active=check_active,max_output_bytes=max_output_bytes)
