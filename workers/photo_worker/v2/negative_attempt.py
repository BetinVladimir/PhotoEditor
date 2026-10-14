"""Repeated negative attempt and qualification gate; source remains separate."""
from copy import deepcopy
from hashlib import sha256
from .negative_qualification import NegativeQualificationPin
from .adoption import AdoptionInvalid


def negative_attempt_checkpoint(database,lease,params_raw,qualification_authority,check_active):
    if not callable(check_active) or not callable(qualification_authority) or not callable(getattr(database,'negative_lease_ready',None)):
        raise ValueError('NEGATIVE_AUTHORITY')
    owned=deepcopy(lease)
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    body=bytes(params_raw)
    command=owned.command
    if command.operation!='optical.negative' or command.operation_version!='1' or sha256(body).hexdigest()!=command.params.sha256:
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    from photo_worker.contracts.optical_negative import OpticalNegativeParams
    from photo_worker.contracts.base import canonical_bytes
    if canonical_bytes(OpticalNegativeParams.model_validate_json(body))!=body:
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    pin=NegativeQualificationPin.from_value(owned.execution_profile.get('negative_qualification'))
    if pin.runtime_lock_sha!=owned.execution_profile.get('negative_runtime_sha'):
        raise ValueError('NEGATIVE_QUALIFICATION_PROFILE_INVALID')
    def verify():
        check_active()
        if database.negative_lease_ready(deepcopy(owned),body) is not True:
            raise AdoptionInvalid('NEGATIVE_ATTEMPT_STALE')
        check_active()
        current=qualification_authority()
        if type(current) is not NegativeQualificationPin or current!=pin:
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_CHANGED')
        check_active()
    return verify
