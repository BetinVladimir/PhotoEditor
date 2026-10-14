"""Completed-result catalog proof; callers separately verify object bytes/color/scene."""
from copy import deepcopy
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.semantic import SemanticTransferParams
from .adoption import AdoptionInvalid


def verify_semantic_adoption_catalog(connection,tenant_id,task_id,context,params,images,roots,masks,*,check_active=lambda:None):
    if not connection.autocommit or not callable(check_active):
        raise ValueError('SEMANTIC_CATALOG_AUTHORITY_CONFIGURATION_INVALID')
    context=deepcopy(context)
    params=SemanticTransferParams.model_validate_json(canonical_bytes(params))
    if not all(isinstance(values,(list,tuple)) and len(values)==2 for values in (images,roots,masks)):
        raise AdoptionInvalid('SEMANTIC_CATALOG_BINDING_INVALID')
    images=tuple(WorkingImage.model_validate_json(canonical_bytes(value)) for value in images)
    roots=tuple(ArtifactRef.model_validate_json(canonical_bytes(value)) for value in roots)
    masks=tuple(MaskRef.model_validate_json(canonical_bytes(value)) for value in masks)
    bindings=(params.reference,params.target)
    for binding,image,mask in zip(bindings,images,masks):
        if (canonical_hash(image.canvas)!=binding.canvas_hash or not mask.protected
                or mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain):
            raise AdoptionInvalid('SEMANTIC_CATALOG_BINDING_INVALID')
    scope=(tenant_id,task_id,context.attempt,context.fence,context.result_id)
    for target,(binding,root,mask) in enumerate(zip(bindings,roots,masks)):
        args=(*scope,binding.asset_version_id,binding.source.artifact_id,binding.source.sha256,
            bool(target),root.artifact_id,root.sha256,mask.artifact.artifact_id,mask.artifact.sha256)
        check_active()
        row=connection.execute('SELECT photo.v27_semantic_adoption_protection_origin('+','.join(['%s']*13)+')',args).fetchone()
        if row is None or row[0] is not True:
            raise AdoptionInvalid('SEMANTIC_PROTECTION_ORIGIN_STALE')
        check_active()


def load_semantic_adoption_catalog(connection,tenant_id,task_id,context,reader,params,*,check_active=lambda:None):
    """Load hashed input documents, validate catalog, recheck frozen roots after IO."""
    from hashlib import sha256
    from .adoption import ArtifactRef as ReadRef,MAX_JSON_BYTES
    from .semantic_replay_inputs import load_semantic_protection_refs
    from photo_worker.contracts.base import parse_json
    context=deepcopy(context)
    params=SemanticTransferParams.model_validate_json(canonical_bytes(params))
    def pins():
        check_active()
        return load_semantic_protection_refs(connection,tenant_id,task_id,context.attempt,context.fence,context.result_id)
    roots=pins()
    def document(ref,model):
        ref=ReadRef(ref.artifact_id,ref.sha256)
        check_active();evidence=reader.evidence(ref)
        if evidence.ref!=ref or evidence.source!='input' or evidence.media_type!='application/json':
            raise AdoptionInvalid('SEMANTIC_CATALOG_EVIDENCE_INVALID')
        raw=reader.read_json(ref,MAX_JSON_BYTES);check_active()
        if (not isinstance(raw,bytes) or len(raw)>MAX_JSON_BYTES or len(raw)!=evidence.bytes
                or sha256(raw).hexdigest()!=ref.sha256):
            raise AdoptionInvalid('SEMANTIC_CATALOG_BYTES_INVALID')
        try:return model.model_validate_json(canonical_bytes(parse_json(raw,MAX_JSON_BYTES)))
        except ValueError as error:raise AdoptionInvalid('SEMANTIC_CATALOG_CONTRACT_INVALID') from error
    images=tuple(document(binding.source,WorkingImage) for binding in (params.reference,params.target))
    masks=tuple(document(root,MaskRef) for root in roots)
    contract_roots=tuple(ArtifactRef(artifact_id=root.artifact_id,sha256=root.sha256) for root in roots)
    verify_semantic_adoption_catalog(connection,tenant_id,task_id,context,params,images,contract_roots,masks,check_active=check_active)
    if pins()!=roots:raise AdoptionInvalid('SEMANTIC_PROTECTION_INPUTS_STALE')
    check_active()
    return images,masks
