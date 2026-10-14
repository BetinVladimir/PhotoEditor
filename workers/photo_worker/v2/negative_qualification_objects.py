"""Original versioned negative qualification IO outside catalog transactions."""
from copy import deepcopy
from hashlib import sha256
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes,parse_json
from .negative_qualification_catalog import resolve_negative_qualification_objects
from .negative_qualification import verify_negative_qualification,NegativeQualificationPin
from .adoption import AdoptionInvalid


@dataclass(frozen=True)
class LocatedNegativeQualification:
    pin: NegativeQualificationPin
    calibration_raw: bytes
    report_raw: bytes
    locations_jcs: bytes


def load_negative_qualification_objects(connection,objects,tenant,project,actor,session,
        params,pin_value,policy,*,bucket,input_icc_sha,runtime_sha,check_active):
    if not callable(check_active) or not isinstance(bucket,str) or not bucket or not callable(getattr(objects,'get_object',None)):
        raise ValueError('NEGATIVE_OBJECT_CONFIGURATION')
    p=deepcopy(params);pin=deepcopy(pin_value);trusted=deepcopy(policy)
    def current():return resolve_negative_qualification_objects(connection,tenant,project,actor,session,pin,check_active=check_active)
    locations=current();baseline=canonical_bytes(locations)
    if any(item['bucket']!=bucket for item in locations):raise AdoptionInvalid('NEGATIVE_OBJECT_BUCKET')
    def checkpoint():
        if canonical_bytes(current())!=baseline:raise AdoptionInvalid('NEGATIVE_QUALIFICATION_CHANGED')
        check_active()
    bodies=[]
    for item in parse_json(baseline,65536):
        checkpoint()
        response=objects.get_object(bucket,item['object_key'],version_id=item['object_version'])
        try:
            raw=bytearray()
            while len(raw)<=item['bytes']:
                check_active();block=response.read(min(65536,item['bytes']+1-len(raw)))
                if not isinstance(block,bytes):raise AdoptionInvalid('NEGATIVE_OBJECT_STREAM')
                if not block:break
                raw.extend(block)
            if len(raw)!=item['bytes']:raise AdoptionInvalid('NEGATIVE_OBJECT_SIZE')
            body=bytes(raw)
            if sha256(body).hexdigest()!=item['sha256']:raise AdoptionInvalid('NEGATIVE_OBJECT_HASH')
            if canonical_bytes(parse_json(body,item['bytes']))!=body:
                raise AdoptionInvalid('NEGATIVE_OBJECT_NONCANONICAL')
            if item['contract']=='NegativeCalibration':
                from photo_worker.operations.optical.negative_calibration import verify_negative_calibration
                verify_negative_calibration(p,body,input_icc_sha=input_icc_sha)
            bodies.append(body)
        finally:
            try:response.close()
            finally:
                if callable(getattr(response,'release_conn',None)):response.release_conn()
        checkpoint()
    verified,_=verify_negative_qualification(p,bodies[0],bodies[1],pin,trusted,
        input_icc_sha=input_icc_sha,runtime_sha=runtime_sha,check_active=check_active)
    checkpoint()
    return LocatedNegativeQualification(verified,bodies[0],bodies[1],baseline)
