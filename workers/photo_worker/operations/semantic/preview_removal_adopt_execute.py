"""Receipt-admitted shown-pixel copy execution; no global operation registration."""
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit,TransferCandidate,EditIntent
from photo_worker.contracts.semantic_preview import (ShownRemovalAdoptionInput,
    RemovalPreviewPresentation,RemovalPreviewNumericalEvidence)
from .preview_removal_adopt_binding import bind_shown_removal_adoption
from .preview_adopt_stage import stage_shown_preview_image


def execute_shown_removal_adoption(artifacts,params,*,verify_admission,verify_runtime,
        verify_color,check_active,max_output_bytes,expected_pixel_count):
    if not all(callable(fn) for fn in (verify_admission,verify_runtime,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=ShownRemovalAdoptionInput.model_validate_json(canonical_bytes(params))
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_DENIED')
        check_active()
        return True
    def document(ref, model):
        admitted()
        raw = artifacts.contract_bytes(ref.model_copy(deep=True), model)
        admitted()
        return raw
    admitted()
    approved=document(owned.approved_edit,ApprovedSemanticEdit)
    presentation=document(owned.presentation,RemovalPreviewPresentation)
    evidence=document(owned.numerical_evidence,RemovalPreviewNumericalEvidence)
    image_raw=document(owned.working_image,WorkingImage)
    preview=RemovalPreviewPresentation.model_validate_json(presentation).params
    # Every dynamically expanded reference must already be in the frozen lease
    # inventory; TaskArtifacts rejects arbitrary undisclosed artifact reads.
    candidate=document(preview.candidate,TransferCandidate)
    intent=document(preview.intent,EditIntent)
    source=document(preview.source.source,WorkingImage)
    _,_,_,image=bind_shown_removal_adoption(owned,approved,presentation,evidence,image_raw,
        candidate,intent,source)
    if type(expected_pixel_count) is not int or image.canvas.width*image.canvas.height!=expected_pixel_count:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    admitted()
    if verify_color(image.color.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')
    admitted()
    forbidden = {ref.artifact_id for ref in (owned.approved_edit, owned.presentation,
        owned.numerical_evidence, owned.working_image, owned.pixels)}
    def visit(value):
        if isinstance(value, dict):
            if set(value) == {'artifact_id', 'sha256'}:
                forbidden.add(ArtifactRef.model_validate_json(canonical_bytes(value)).artifact_id)
            else:
                for child in value.values():
                    visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)
    for raw in (approved, presentation, evidence, image_raw, candidate, intent, source):
        visit(parse_json(raw))
    result = stage_shown_preview_image(artifacts,owned.working_image,image_raw,
        verify_admission=admitted,check_active=check_active,max_output_bytes=max_output_bytes)
    admitted()
    if (result.image.artifact_id in forbidden or result.pixels.artifact_id in forbidden
            or result.image.artifact_id == result.pixels.artifact_id):
        raise ValueError('SEMANTIC_PREVIEW_ADOPTION_OUTPUT_ALIAS')
    return result
