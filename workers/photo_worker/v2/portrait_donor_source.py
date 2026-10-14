"""Leased original donor proof bound to live server lineage and current consent."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.contracts.donor_confirmation import verify_open_eyes_confirmation
from .adoption import AdoptionInvalid


def donor_source_authority(database,lease,params_raw,check_active):
    if not callable(check_active) or not callable(getattr(database,'donor_source_ready',None)):
        raise ValueError('DONOR_AUTHORITY')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('DONOR_PARAMS_INVALID')
    body=bytes(params_raw);owned=deepcopy(lease)
    if (owned.command.operation!='portrait.open_eyes' or owned.command.operation_version!='1'
            or sha256(body).hexdigest()!=owned.command.params.sha256):
        raise ValueError('DONOR_PARAMS_INVALID')
    expected=PortraitOpenEyesParams.model_validate_json(body)
    if canonical_bytes(expected)!=body:raise ValueError('DONOR_PARAMS_INVALID')
    def verify(params,confirmation_raw):
        check_active()
        if not isinstance(params,PortraitOpenEyesParams) or canonical_bytes(params)!=body:
            raise AdoptionInvalid('DONOR_SOURCE_PARAMS_CHANGED')
        if not isinstance(confirmation_raw,bytes):raise ValueError('DONOR_PROOF_INVALID')
        proof=bytes(confirmation_raw)
        verify_open_eyes_confirmation(expected,proof)
        check_active()
        if database.donor_source_ready(deepcopy(owned),body,proof) is not True:
            raise AdoptionInvalid('DONOR_SOURCE_AUTHORITY_STALE')
        check_active()
        return True
    return verify
