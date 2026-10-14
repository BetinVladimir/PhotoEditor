"""Original negative qualification objects with live leased SQL revalidation."""
from contextlib import closing
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from .negative_qualification import NegativeQualificationPin,verify_negative_qualification
from .adoption import AdoptionInvalid


def load_negative_qualification(database,lease,params_raw,objects,bucket,*,input_icc_sha,check_active):
    if (not callable(check_active) or not isinstance(bucket,str) or not 1<=len(bucket)<=255
            or not callable(getattr(database,'negative_qualification_evidence',None))
            or not callable(getattr(objects,'get_object',None))):
        raise ValueError('NEGATIVE_AUTHORITY')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    owned=deepcopy(lease);body=bytes(params_raw)
    if (owned.command.operation!='optical.negative' or owned.command.operation_version!='1'
            or sha256(body).hexdigest()!=owned.command.params.sha256):raise ValueError('NEGATIVE_PARAMS_INVALID')
    params=OpticalNegativeParams.model_validate_json(body)
    if canonical_bytes(params)!=body:raise ValueError('NEGATIVE_PARAMS_INVALID')
    profile=parse_json(canonical_bytes(owned.execution_profile),1<<20)
    frozen=NegativeQualificationPin.from_value(profile.get('negative_qualification'))
    def evidence():
        check_active()
        record=parse_json(canonical_bytes(database.negative_qualification_evidence(deepcopy(owned),body)),8<<20)
        check_active()
        if not isinstance(record,dict) or set(record)!={'pin','policy','report','calibration'} or NegativeQualificationPin.from_value(record['pin'])!=frozen:
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_SCOPE_STALE')
        return record
    if frozen.runtime_lock_sha!=profile.get('negative_runtime_sha') or params.calibration.artifact_id!=frozen.calibration.artifact_id or params.calibration.sha256!=frozen.calibration.sha256:
        raise AdoptionInvalid('NEGATIVE_QUALIFICATION_PROFILE_INVALID')
    record=evidence();retained=canonical_bytes(record);bodies={}
    def current_pin():
        current=evidence()
        if canonical_bytes(current)!=retained:raise AdoptionInvalid('NEGATIVE_QUALIFICATION_SCOPE_STALE')
        return current['pin']
    for label,ref,maximum in [('report',frozen.report,8<<20),('calibration',frozen.calibration,65536)]:
        location=record[label]
        if (not isinstance(location,dict) or set(location)!={'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'}
                or location['artifact_id']!=str(ref.artifact_id) or location['sha256']!=ref.sha256
                or location['bucket']!=bucket or location['media_type']!='application/json'
                or type(location['bytes']) is not int or not 2<=location['bytes']<=maximum
                or not isinstance(location['object_key'],str) or not 1<=len(location['object_key'])<=1024
                or not isinstance(location['object_version'],str) or not 1<=len(location['object_version'])<=1024 or location['object_version']=='null'):
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_OBJECT_INVALID')
    for label,ref,maximum in [('calibration',frozen.calibration,65536),('report',frozen.report,8<<20)]:
        location=record[label]
        current_pin();check_active();response=objects.get_object(bucket,location['object_key'],version_id=location['object_version'])
        try:
            with closing(response):
                raw=bytearray()
                while len(raw)<=location['bytes']:
                    check_active();block=response.read(min(65536,location['bytes']+1-len(raw)))
                    if not isinstance(block,bytes):raise AdoptionInvalid('NEGATIVE_QUALIFICATION_STREAM_INVALID')
                    if not block:break
                    raw.extend(block)
                if len(raw)!=location['bytes']:raise AdoptionInvalid('NEGATIVE_QUALIFICATION_SIZE_CHANGED')
                original=bytes(raw)
                if sha256(original).hexdigest()!=location['sha256'] or canonical_bytes(parse_json(original,maximum))!=original:
                    raise AdoptionInvalid('NEGATIVE_QUALIFICATION_BYTES_INVALID')
                if label=='calibration':
                    from photo_worker.operations.optical.negative_calibration import verify_negative_calibration
                    verify_negative_calibration(params,original,input_icc_sha=input_icc_sha)
                bodies[label]=original
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()
        current_pin()
    verify_negative_qualification(params,bodies['calibration'],bodies['report'],record['pin'],record['policy'],
        input_icc_sha=input_icc_sha,runtime_sha=frozen.runtime_lock_sha,check_active=check_active)
    current_pin()
    return bodies['report'],bodies['calibration'],deepcopy(record['policy']),current_pin
