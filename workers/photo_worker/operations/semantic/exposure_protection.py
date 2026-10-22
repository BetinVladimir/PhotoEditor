"""Pinned target protection document with mandatory canonical-origin proof."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from .exposure_source import bind_exposure_working_source
from photo_worker.operations.session.contracts import VersionBinding


def bind_local_exposure_protection(edit,source_raw,protection_ref,protection_raw,*,verify_origin,check_active):
    if not callable(verify_origin) or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_INSTALLATION_REQUIRED')
    owned=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    return bind_exposure_protection_source(owned.source,source_raw,protection_ref,protection_raw,
        verify_origin=verify_origin,check_active=check_active)


def bind_exposure_protection_source(source,source_raw,protection_ref,protection_raw,*,verify_origin,check_active):
    """Shared exact protection proof; canonical origin is mandatory for all callers."""
    if not callable(verify_origin) or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_INSTALLATION_REQUIRED')
    source=VersionBinding.model_validate_json(canonical_bytes(source))
    image=bind_exposure_working_source(source,source_raw)
    root=ArtifactRef.model_validate_json(canonical_bytes(protection_ref))
    if (root.artifact_id.int==0 or not isinstance(protection_raw,(bytes,bytearray))
            or not 2<=len(protection_raw)<=1048576):
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_INVALID')
    raw=bytes(protection_raw)
    if sha256(raw).hexdigest()!=root.sha256:
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_HASH')
    mask=MaskRef.model_validate_json(canonical_bytes(parse_json(raw)))
    if (not mask.protected or mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain
            or mask.artifact.artifact_id.int==0
            or (mask.model_lock_id is None and mask.selector!='manual')):
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_CHANGED')
    # Callbacks receive independent values and cannot substitute returned pins.
    check_active()
    if verify_origin(source.model_copy(deep=True),root.model_copy(deep=True),mask.artifact.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_ORIGIN_STALE')
    check_active()
    return mask


def sql_local_exposure_protection_origin(database,lease,params_raw,*,check_active):
    """Install immutable task/source-bound origin callback for protection loading."""
    from copy import deepcopy
    authority=getattr(database,'local_exposure_protection_origin',None)
    if not callable(authority) or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_INSTALLATION_REQUIRED')
    owned_lease=deepcopy(lease)
    command=owned_lease.command
    if (not isinstance(params_raw,(bytes,bytearray)) or not 2<=len(params_raw)<=1048576
            or command.operation!='semantic.local_exposure' or command.operation_version!='1'
            or command.resource_class!='cpu' or owned_lease.execution_profile.get('backend')!='cpu'
            or owned_lease.execution_profile.get('renderer_version')!='v2.2'):
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_TASK_INVALID')
    raw=bytes(params_raw)
    if sha256(raw).hexdigest()!=command.params.sha256:
        raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_PARAMS_CHANGED')
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(parse_json(raw)))
    if edit.kind!='local_exposure':
        raise ValueError('SEMANTIC_EXPOSURE_KIND_INVALID')
    binding_bytes=canonical_bytes(edit.source)
    def origin(binding,root,binary):
        check_active()
        if canonical_bytes(binding)!=binding_bytes:
            return False
        root=ArtifactRef.model_validate_json(canonical_bytes(root))
        binary=ArtifactRef.model_validate_json(canonical_bytes(binary))
        if root.artifact_id.int==0 or binary.artifact_id.int==0:
            return False
        accepted=authority(deepcopy(owned_lease),raw,root,binary) is True
        check_active()
        return accepted
    return origin
