"""Owned transfer params and live leased SQL catalog/source preflight."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json
from photo_worker.contracts.semantic import SemanticTransferParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import AdoptionInvalid


def semantic_input_authority(database,lease,params_bytes,*,verify_color,check_active=lambda:None):
    """Return PinnedSemanticArrayLoader.verify_inputs without permissive defaults.

    Artifact loading separately establishes exact image object bytes. The caller
    must supply trusted ICC/color verification; mask origin and scene proof are
    distinct installer requirements.
    """
    if not callable(verify_color) or not callable(check_active) or not isinstance(params_bytes,bytes):
        raise ValueError('SEMANTIC_SOURCE_AUTHORITY_INVALID')
    owned_lease=deepcopy(lease)
    if sha256(params_bytes).hexdigest()!=owned_lease.command.params.sha256:
        raise AdoptionInvalid('SEMANTIC_PARAMS_CHANGED')
    params=SemanticTransferParams.model_validate_json(canonical_bytes(parse_json(params_bytes,1048576)))
    frozen=canonical_bytes(params)
    def verify(value,reference,target):
        check_active()
        current=SemanticTransferParams.model_validate_json(canonical_bytes(value))
        if canonical_bytes(current)!=frozen:raise AdoptionInvalid('SEMANTIC_PARAMS_CHANGED')
        images=tuple(WorkingImage.model_validate_json(canonical_bytes(image)) for image in (reference,target))
        for binding,image,is_target in zip((current.reference,current.target),images,(False,True)):
            check_active()
            if canonical_hash(image.canvas)!=binding.canvas_hash:
                raise AdoptionInvalid('SEMANTIC_SOURCE_CHANGED')
            if database.semantic_source_ready(deepcopy(owned_lease),deepcopy(binding),target=is_target) is not True:
                raise AdoptionInvalid('SEMANTIC_SOURCE_SCOPE_STALE')
            check_active();verify_color(deepcopy(image));check_active()
    return verify
