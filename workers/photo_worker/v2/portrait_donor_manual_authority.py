"""Repeated SQL origin binding for source-frame donor manual masks."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json,ArtifactRef
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.contracts.manual_mask import MaskRasterParams
from .portrait_donor_images import verify_donor_working_image
from .portrait_donor_masks import verify_donor_mask
from .adoption import AdoptionInvalid


def donor_manual_authority(database,lease,params_raw,source_raw,target_raw,protected_raw,
        root_ref,producer_ref,producer_raw,check_active):
    if not callable(check_active) or not callable(getattr(database,'donor_manual_origin',None)):
        raise ValueError('DONOR_MANUAL_AUTHORITY')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('DONOR_PARAMS_INVALID')
    owned=deepcopy(lease);body=bytes(params_raw)
    if sha256(body).hexdigest()!=owned.command.params.sha256 or owned.command.operation!='portrait.open_eyes' or owned.command.operation_version!='1':
        raise ValueError('DONOR_PARAMS_INVALID')
    params=PortraitOpenEyesParams.model_validate_json(body)
    if canonical_bytes(params)!=body:raise ValueError('DONOR_PARAMS_INVALID')
    source=verify_donor_working_image(bytes(source_raw),params.base)
    target=verify_donor_mask(bytes(target_raw),params.eyes_mask,source,protected=False)
    guard=verify_donor_mask(bytes(protected_raw),params.protected,source,protected=True)
    root=root_ref.model_copy(deep=True);original=producer_ref.model_copy(deep=True)
    if root.artifact_id.int==0 or original.artifact_id.int==0 or root.artifact_id==original.artifact_id:
        raise ValueError('DONOR_MANUAL_ORIGIN_INVALID')
    raw=bytes(producer_raw);parse_json(raw,1<<20)
    producer=MaskRasterParams.model_validate_json(raw)
    if canonical_bytes(producer)!=raw or sha256(raw).hexdigest()!=original.sha256 or producer.image!=params.base.source:
        raise ValueError('DONOR_MANUAL_ORIGIN_INVALID')
    vectors=None if producer.vectors is None else producer.vectors.model_copy(deep=True)
    def verify(current_params,current_root,current_producer):
        check_active()
        if canonical_bytes(current_params)!=body or current_root!=root or current_producer!=original:
            raise AdoptionInvalid('DONOR_MANUAL_ORIGIN_CHANGED')
        row=database.donor_manual_origin(deepcopy(owned),body,target.artifact.model_copy(deep=True),
            guard.artifact.model_copy(deep=True),None if vectors is None else vectors.model_copy(deep=True))
        check_active()
        if not isinstance(row,(tuple,list)) or len(row)!=4:
            raise AdoptionInvalid('DONOR_MANUAL_ORIGIN_STALE')
        if ArtifactRef(artifact_id=row[0],sha256=row[1])!=root or ArtifactRef(artifact_id=row[2],sha256=row[3])!=original:
            raise AdoptionInvalid('DONOR_MANUAL_ORIGIN_CHANGED')
        return True
    return verify
