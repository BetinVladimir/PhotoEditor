"""Own retouch report resolver, pinned to task Params and adapter-context bytes."""
from copy import deepcopy
from hashlib import sha256
from types import SimpleNamespace
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from .adoption import AdoptionInvalid
from .retouch_qualification_pin import RetouchQualificationPin
from .retouch_qualification_report import verify_retouch_qualification_report
from .semantic_qualification_loader import _qualification_loader


def retouch_qualification_loader(database,lease,objects,bucket,*,params,context_ref,context_raw,
        frozen_pin,check_active,declared_inputs):
    if (type(frozen_pin) is not RetouchQualificationPin or not callable(check_active)
            or not callable(getattr(database,'retouch_preview_report_evidence',None))):
        raise ValueError('SEMANTIC_RETOUCH_QUALIFICATION_INSTALLATION_REQUIRED')
    if not isinstance(declared_inputs,tuple) or not 1<=len(declared_inputs)<=512:
        raise AdoptionInvalid('SEMANTIC_RETOUCH_QUALIFICATION_SCOPE_STALE')
    inputs=tuple(ArtifactRef.model_validate_json(canonical_bytes(value)) for value in declared_inputs)
    if any(value.artifact_id.int==0 for value in inputs) or len({value.artifact_id for value in inputs})!=len(inputs):
        raise AdoptionInvalid('SEMANTIC_RETOUCH_QUALIFICATION_SCOPE_STALE')
    owner=deepcopy(lease)
    owned=SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    ref=ArtifactRef.model_validate_json(canonical_bytes(context_ref))
    if (not isinstance(context_raw,bytes) or not 2<=len(context_raw)<=1048576
            or sha256(context_raw).hexdigest()!=ref.sha256):
        raise AdoptionInvalid('SEMANTIC_RETOUCH_CONTEXT_BYTES_INVALID')
    context=RetouchPreviewAdapterContext.model_validate_json(context_raw)
    raw=canonical_bytes(owned);body=bytes(context_raw)
    command=owner.command
    if (canonical_bytes(context)!=body or context.source!=owned.source or context.candidate!=owned.candidate
            or context.protection!=owned.protection
            or (context.model_lock_sha,context.runtime_lock_sha,context.device_profile_sha)!=(
                owned.model_lock_sha,owned.runtime_lock_sha,owned.device_profile_sha)
            or (frozen_pin.model_lock_sha,frozen_pin.runtime_lock_sha,frozen_pin.device_profile_sha)!=(
                owned.model_lock_sha,owned.runtime_lock_sha,owned.device_profile_sha)
            or command.operation!='semantic.preview_retouch_region' or command.operation_version!='1'
            or command.tenant_id!=owned.tenant_id or command.params.sha256!=sha256(raw).hexdigest()
            or ref.artifact_id.int==0 or not any(value==ref for value in inputs)):
        raise AdoptionInvalid('SEMANTIC_RETOUCH_QUALIFICATION_SCOPE_STALE')
    def evidence(supplied):
        check_active()
        if supplied!=owner:raise AdoptionInvalid('SEMANTIC_RETOUCH_QUALIFICATION_SCOPE_STALE')
        record=database.retouch_preview_report_evidence(deepcopy(owner),raw,ref.model_copy(deep=True),body)
        check_active()
        return record
    return _qualification_loader(SimpleNamespace(semantic_report_evidence=evidence),owner,objects,bucket,
        frozen_pin=frozen_pin,pin_type=RetouchQualificationPin,
        verify_report=verify_retouch_qualification_report,check_active=check_active)
