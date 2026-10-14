"""Repeated donor attempt and qualification gate; source/consent remains separate."""
from copy import deepcopy
from hashlib import sha256
from .portrait_donor_qualification import DonorQualificationPin
from .adoption import AdoptionInvalid


def donor_attempt_checkpoint(database,lease,params_raw,qualification_authority,check_active):
    if not callable(check_active) or not callable(qualification_authority) or not callable(getattr(database,'donor_portrait_lease_ready',None)):
        raise ValueError('DONOR_AUTHORITY')
    owned=deepcopy(lease)
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('DONOR_PARAMS_INVALID')
    body=bytes(params_raw)
    command=owned.command
    if command.operation!='portrait.open_eyes' or command.operation_version!='1' or sha256(body).hexdigest()!=command.params.sha256:
        raise ValueError('DONOR_PARAMS_INVALID')
    pin=DonorQualificationPin.from_value(owned.execution_profile.get('donor_qualification'))
    if pin.runtime_lock_sha!=owned.execution_profile.get('portrait_runtime_sha'):
        raise ValueError('DONOR_QUALIFICATION_PROFILE_INVALID')
    def verify():
        check_active()
        if database.donor_portrait_lease_ready(deepcopy(owned),body) is not True:
            raise AdoptionInvalid('DONOR_ATTEMPT_STALE')
        check_active()
        current=qualification_authority()
        if type(current) is not DonorQualificationPin or current!=pin:
            raise AdoptionInvalid('DONOR_QUALIFICATION_CHANGED')
        check_active()
    return verify
