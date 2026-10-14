"""Typed source-bound protection proof before semantic array consumption."""
from copy import deepcopy
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.semantic import SemanticTransferParams
from photo_worker.v2.adoption import AdoptionInvalid


def semantic_protection_authority(load_document,verify_origin,*,check_active=lambda:None):
    """Build the required protection verifier for the qualified SQL installer.

    load_document must hash/version-verify the exact MaskRef object. verify_origin
    must prove its binary child is a current canonical output bound to the exact
    VersionBinding, returning literal True. Neither callback has a fallback.
    Pixel range/shape and overlap remain enforced by array/transfer processing.
    """
    if not all(callable(value) for value in (load_document,verify_origin,check_active)):
        raise ValueError('SEMANTIC_PROTECTION_AUTHORITY_INVALID')
    def verify(params,reference,target,source_ref,target_ref):
        owned=SemanticTransferParams.model_validate_json(canonical_bytes(params))
        images=tuple(WorkingImage.model_validate_json(canonical_bytes(value)) for value in (reference,target))
        refs=tuple(ArtifactRef.model_validate_json(canonical_bytes(value)) for value in (source_ref,target_ref))
        masks=[]
        for ref,image in zip(refs,images):
            check_active()
            mask=MaskRef.model_validate_json(canonical_bytes(load_document(deepcopy(ref),MaskRef)))
            if not mask.protected or mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain:
                raise AdoptionInvalid('SEMANTIC_PROTECTION_BINDING_INVALID')
            masks.append(mask)
        for binding,ref,mask in zip((owned.reference,owned.target),refs,masks):
            check_active()
            if verify_origin(deepcopy(binding),deepcopy(ref),deepcopy(mask.artifact)) is not True:
                raise AdoptionInvalid('SEMANTIC_PROTECTION_ORIGIN_STALE')
            check_active()
    return verify


def sql_semantic_protection_authority(database,lease,params_bytes,load_document,*,check_active=lambda:None):
    """Connect exact source-bound canonical producer proof to typed masks."""
    from hashlib import sha256
    from photo_worker.contracts.base import parse_json
    owned_lease=deepcopy(lease)
    if not isinstance(params_bytes,bytes) or sha256(params_bytes).hexdigest()!=owned_lease.command.params.sha256:
        raise AdoptionInvalid('SEMANTIC_PARAMS_CHANGED')
    params=SemanticTransferParams.model_validate_json(canonical_bytes(parse_json(params_bytes,1048576)))
    frozen=canonical_bytes(params)
    def origin(binding,root,child):
        for expected,target in ((params.reference,False),(params.target,True)):
            if canonical_bytes(binding)==canonical_bytes(expected):
                check_active()
                if database.semantic_protection_origin(deepcopy(owned_lease),deepcopy(binding),
                        deepcopy(root),deepcopy(child),target=target) is True:
                    return True
        return False
    authority=semantic_protection_authority(load_document,origin,check_active=check_active)
    def verify(value,*args):
        if canonical_bytes(value)!=frozen:raise AdoptionInvalid('SEMANTIC_PARAMS_CHANGED')
        authority(value,*args)
    return verify
