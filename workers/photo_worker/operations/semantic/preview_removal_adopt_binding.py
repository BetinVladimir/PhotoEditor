"""Bind an approved copy task to the exact shown canonical preview documents."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.semantic_preview import (ShownRemovalAdoptionInput,
    RemovalPreviewPresentation,RemovalPreviewNumericalEvidence)
from .preview_binding import bind_removal_preview_documents,bind_removal_preview_source


def bind_shown_removal_adoption(params,approved_raw,presentation_raw,evidence_raw,image_raw,
        candidate_raw,intent_raw,source_raw):
    owned=ShownRemovalAdoptionInput.model_validate_json(canonical_bytes(params))
    def document(ref,raw,model):
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=ref.sha256:
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_DOCUMENT_INVALID')
        value = model.model_validate_json(raw)
        if canonical_bytes(value) != raw:
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_DOCUMENT_INVALID')
        return value
    edit=document(owned.approved_edit,approved_raw,ApprovedSemanticEdit)
    presentation=document(owned.presentation,presentation_raw,RemovalPreviewPresentation)
    evidence=document(owned.numerical_evidence,evidence_raw,RemovalPreviewNumericalEvidence)
    image=document(owned.working_image,image_raw,WorkingImage)
    preview,candidate,intent=bind_removal_preview_documents(presentation.params,candidate_raw,intent_raw)
    source=bind_removal_preview_source(preview,source_raw)
    if (canonical_bytes(evidence.params)!=canonical_bytes(preview)
            or preview.tenant_id!=owned.tenant_id or preview.target_id!=owned.target_id
            or edit.target_id!=owned.target_id or edit.source!=preview.source
            or edit.intent!=preview.intent or edit.candidate!=preview.candidate
            or edit.mask!=candidate.mask or edit.transform!=candidate.transform
            or edit.kind!='remove_object' or edit.kind!=intent.kind
            or edit.ev!=intent.ev or edit.strength!=intent.strength
            or edit.prompt!=intent.prompt or edit.preserve_persons is not True
            or presentation.numerical_evidence!=owned.numerical_evidence
            or presentation.working_image!=owned.working_image or presentation.pixels!=owned.pixels
            or evidence.working_image!=owned.working_image or evidence.pixels!=owned.pixels
            or image.pixels!=owned.pixels
            or (owned.model_lock_sha,owned.runtime_lock_sha,owned.device_profile_sha)!=(
                preview.model_lock_sha,preview.runtime_lock_sha,preview.device_profile_sha)
            or canonical_bytes(image.model_copy(update={'pixels':source.pixels}))!=canonical_bytes(source)):
        raise ValueError('SEMANTIC_PREVIEW_ADOPTION_BINDING_INVALID')
    roots=(preview.plan,preview.intent,preview.candidate,preview.source.source,preview.protection,
        owned.approved_edit,owned.presentation,owned.numerical_evidence,owned.working_image,owned.pixels,
        source.pixels,presentation.before,presentation.after,presentation.affected,presentation.protected)
    if (any(ref.artifact_id.int == 0 for ref in roots)
            or len({ref.artifact_id for ref in roots})!=len(roots)
            or source.alpha is not None and source.alpha.artifact_id in {ref.artifact_id for ref in roots}):
        raise ValueError('SEMANTIC_PREVIEW_ADOPTION_BINDING_INVALID')
    return owned,edit,preview,image
