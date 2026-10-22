"""Owned content binding only. SQL authority, qualification and IO remain external."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.semantic import TransferCandidate, EditIntent
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput


def bind_preview_proposal_documents(params, candidate_raw, intent_raw, *, input_model, kind):
    """Shared byte/scope binder; caller supplies a closed typed operation model."""
    if isinstance(params,dict) and type(params.get('schema_version')) is not int:
        raise ValueError('SEMANTIC_PREVIEW_DOCUMENT_SCHEMA')
    owned = input_model.model_validate_json(canonical_bytes(params))
    def document(raw, ref, model):
        if not isinstance(raw,(bytes,bytearray)) or not 2 <= len(raw) <= 1_048_576:
            raise ValueError('SEMANTIC_PREVIEW_DOCUMENT_INVALID')
        body = bytes(raw)
        if sha256(body).hexdigest() != ref.sha256:
            raise ValueError('SEMANTIC_PREVIEW_DOCUMENT_HASH')
        value = parse_json(body)
        if not isinstance(value,dict) or type(value.get('schema_version')) is not int or value['schema_version'] != 1:
            raise ValueError('SEMANTIC_PREVIEW_DOCUMENT_SCHEMA')
        if model is EditIntent and value.get('preserve_persons') is not True:
            raise ValueError('SEMANTIC_PREVIEW_INTENT_PROTECTION_INVALID')
        return model.model_validate_json(canonical_bytes(value))
    candidate = document(candidate_raw,owned.candidate,TransferCandidate)
    intent = document(intent_raw,owned.intent,EditIntent)
    if (candidate.state != 'proposed' or candidate.reason is not None
            or candidate.target != owned.source or candidate.mask is None
            or candidate.transform is None or candidate.mask.artifact_id.int == 0
            or candidate.transform.artifact_id.int == 0):
        raise ValueError('SEMANTIC_PREVIEW_CANDIDATE_BINDING')
    refs = (owned.plan,owned.intent,owned.candidate,owned.source.source,
            owned.protection,candidate.mask,candidate.transform)
    if len({ref.artifact_id for ref in refs}) != len(refs):
        raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
    if (intent.kind != kind or kind == 'local_exposure' and intent.ev is None
            or intent.target_snapshot_id != owned.snapshot_id or not intent.preserve_persons):
        raise ValueError('SEMANTIC_PREVIEW_INTENT_BINDING')
    return owned,candidate,intent


def bind_exposure_preview_documents(params, candidate_raw, intent_raw):
    return bind_preview_proposal_documents(params,candidate_raw,intent_raw,
        input_model=SemanticExposurePreviewInput,kind='local_exposure')


def bind_removal_preview_documents(params, candidate_raw, intent_raw):
    from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
    return bind_preview_proposal_documents(params,candidate_raw,intent_raw,
        input_model=SemanticRemovalPreviewInput,kind='remove_object')


def bind_preview_proposal_source(params, source_raw, *, input_model):
    """Bind WorkingImage without compiling or authorizing an approved edit."""
    from .exposure_source import bind_exposure_working_source
    owned=input_model.model_validate_json(canonical_bytes(params))
    image=bind_exposure_working_source(owned.source,source_raw)
    document_ids={ref.artifact_id for ref in
        (owned.plan,owned.intent,owned.candidate,owned.source.source,owned.protection)}
    if image.pixels.artifact_id in document_ids or (image.alpha is not None and
            (image.alpha.artifact_id in document_ids or image.alpha.artifact_id==image.pixels.artifact_id)):
        raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
    return image


def bind_preview_proposal_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw,*,input_model,kind):
    from .exposure_geometry import bind_exposure_geometry_documents
    owned,candidate,intent=bind_preview_proposal_documents(params,candidate_raw,intent_raw,input_model=input_model,kind=kind)
    # Also enforce preview-specific document/pixel alias separation.
    bind_preview_proposal_source(owned,source_raw,input_model=input_model)
    image,mask,transform=bind_exposure_geometry_documents(owned.source,candidate,intent,
        source_raw,mask_raw,transform_raw,reference_raw)
    document_ids={ref.artifact_id for ref in (owned.plan,owned.intent,owned.candidate,
        owned.source.source,owned.protection,candidate.mask,candidate.transform)}
    if mask.artifact.artifact_id in document_ids or mask.artifact.artifact_id==image.pixels.artifact_id:
        raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
    return image,mask,transform


def bind_preview_proposal_protection(params,source_raw,protection_raw,*,input_model,verify_origin,check_active):
    from .exposure_protection import bind_exposure_protection_source
    owned=input_model.model_validate_json(canonical_bytes(params))
    image=bind_preview_proposal_source(owned,source_raw,input_model=input_model)
    mask=bind_exposure_protection_source(owned.source,source_raw,owned.protection,protection_raw,
        verify_origin=verify_origin,check_active=check_active)
    document_ids={ref.artifact_id for ref in
        (owned.plan,owned.intent,owned.candidate,owned.source.source,owned.protection)}
    if mask.artifact.artifact_id in document_ids or mask.artifact.artifact_id==image.pixels.artifact_id:
        raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
    return mask


def bind_exposure_preview_source(params,source_raw):
    return bind_preview_proposal_source(params,source_raw,input_model=SemanticExposurePreviewInput)


def bind_exposure_preview_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw):
    return bind_preview_proposal_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw,
        input_model=SemanticExposurePreviewInput,kind='local_exposure')


def bind_exposure_preview_protection(params,source_raw,protection_raw,*,verify_origin,check_active):
    return bind_preview_proposal_protection(params,source_raw,protection_raw,
        input_model=SemanticExposurePreviewInput,verify_origin=verify_origin,check_active=check_active)


def bind_removal_preview_source(params,source_raw):
    from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
    return bind_preview_proposal_source(params,source_raw,input_model=SemanticRemovalPreviewInput)


def bind_removal_preview_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw):
    from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
    return bind_preview_proposal_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw,
        input_model=SemanticRemovalPreviewInput,kind='remove_object')


def bind_removal_preview_protection(params,source_raw,protection_raw,*,verify_origin,check_active):
    from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
    return bind_preview_proposal_protection(params,source_raw,protection_raw,
        input_model=SemanticRemovalPreviewInput,verify_origin=verify_origin,check_active=check_active)


def bind_background_cleanup_preview_documents(params, candidate_raw, intent_raw):
    from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
    return bind_preview_proposal_documents(params,candidate_raw,intent_raw,
        input_model=SemanticBackgroundCleanupPreviewInput,kind='background_cleanup')


def bind_background_cleanup_preview_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw):
    from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
    return bind_preview_proposal_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw,
        input_model=SemanticBackgroundCleanupPreviewInput,kind='background_cleanup')


def bind_background_cleanup_preview_protection(params,source_raw,protection_raw,*,verify_origin,check_active):
    from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
    return bind_preview_proposal_protection(params,source_raw,protection_raw,
        input_model=SemanticBackgroundCleanupPreviewInput,verify_origin=verify_origin,check_active=check_active)


def bind_background_cleanup_preview_source(params,source_raw):
    from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
    return bind_preview_proposal_source(params,source_raw,input_model=SemanticBackgroundCleanupPreviewInput)


def bind_retouch_preview_documents(params,candidate_raw,intent_raw):
    from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
    for raw in (candidate_raw,intent_raw):
        if (not isinstance(raw,(bytes,bytearray)) or not 2<=len(raw)<=1048576
                or canonical_bytes(parse_json(bytes(raw)))!=bytes(raw)):
            raise ValueError('SEMANTIC_RETOUCH_DOCUMENT_BYTES_INVALID')
    return bind_preview_proposal_documents(params,candidate_raw,intent_raw,
        input_model=SemanticRetouchPreviewInput,kind='retouch_region')


def bind_retouch_preview_source(params,source_raw):
    from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
    return bind_preview_proposal_source(params,source_raw,input_model=SemanticRetouchPreviewInput)


def bind_retouch_preview_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw):
    from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
    return bind_preview_proposal_geometry(params,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw,
        input_model=SemanticRetouchPreviewInput,kind='retouch_region')


def bind_retouch_preview_protection(params,source_raw,protection_raw,*,verify_origin,check_active):
    from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
    return bind_preview_proposal_protection(params,source_raw,protection_raw,
        input_model=SemanticRetouchPreviewInput,verify_origin=verify_origin,check_active=check_active)
