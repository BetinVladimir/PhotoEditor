"""Receipt-admitted shown-pixel copy execution; no global operation registration."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit,TransferCandidate,EditIntent
from photo_worker.contracts.semantic_preview import (ShownExposureAdoptionInput,
    ExposurePreviewPresentation,ExposurePreviewNumericalEvidence)
from .preview_adopt_binding import bind_shown_exposure_adoption
from .preview_adopt_stage import stage_shown_preview_image


def execute_shown_exposure_adoption(artifacts,params,*,verify_admission,verify_runtime,
        verify_color,check_active,max_output_bytes,expected_pixel_count):
    if not all(callable(fn) for fn in (verify_admission,verify_runtime,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=ShownExposureAdoptionInput.model_validate_json(canonical_bytes(params))
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_DENIED')
        check_active()
        return True
    admitted()
    approved=artifacts.contract_bytes(owned.approved_edit,ApprovedSemanticEdit)
    presentation=artifacts.contract_bytes(owned.presentation,ExposurePreviewPresentation)
    evidence=artifacts.contract_bytes(owned.numerical_evidence,ExposurePreviewNumericalEvidence)
    image_raw=artifacts.contract_bytes(owned.working_image,WorkingImage)
    preview=ExposurePreviewPresentation.model_validate_json(presentation).params
    # Every dynamically expanded reference must already be in the frozen lease
    # inventory; TaskArtifacts rejects arbitrary undisclosed artifact reads.
    candidate=artifacts.contract_bytes(preview.candidate,TransferCandidate)
    intent=artifacts.contract_bytes(preview.intent,EditIntent)
    source=artifacts.contract_bytes(preview.source.source,WorkingImage)
    _,_,_,image=bind_shown_exposure_adoption(owned,approved,presentation,evidence,image_raw,
        candidate,intent,source)
    if type(expected_pixel_count) is not int or image.canvas.width*image.canvas.height!=expected_pixel_count:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    admitted()
    if verify_color(image.color.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')
    admitted()
    return stage_shown_preview_image(artifacts,owned.working_image,image_raw,
        verify_admission=admitted,check_active=check_active,max_output_bytes=max_output_bytes)
