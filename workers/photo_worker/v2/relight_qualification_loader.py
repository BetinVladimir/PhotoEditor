"""Original relight qualification objects with live leased SQL revalidation."""
from contextlib import closing
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.relight import ImageRelightParams
from .relight_qualification import RelightQualificationPin,verify_relight_qualification
from .adoption import AdoptionInvalid


def load_relight_qualification(database,lease,params_raw,objects,bucket,*,expected_runtime_sha,check_active):
    if not callable(check_active) or not isinstance(bucket,str) or not bucket:
        raise ValueError('RELIGHT_AUTHORITY')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('RELIGHT_PARAMS_INVALID')
    owned=deepcopy(lease);body=bytes(params_raw)
    if (owned.command.operation!='image.relight' or owned.command.operation_version!='1'
            or sha256(body).hexdigest()!=owned.command.params.sha256):raise ValueError('RELIGHT_PARAMS_INVALID')
    params=ImageRelightParams.model_validate_json(body)
    if canonical_bytes(params)!=body:raise ValueError('RELIGHT_PARAMS_INVALID')
    profile=parse_json(canonical_bytes(owned.execution_profile),1<<20)
    frozen=RelightQualificationPin.from_value(profile.get('relight_qualification'))
    if profile.get('relight_runtime_sha')!=expected_runtime_sha:
        raise AdoptionInvalid('RELIGHT_RUNTIME_MISMATCH')
    def evidence():
        check_active()
        record=parse_json(canonical_bytes(database.relight_qualification_evidence(deepcopy(owned),body)),8<<20)
        check_active()
        if not isinstance(record,dict) or set(record)!={'pin','policy','report','calibration'} or RelightQualificationPin.from_value(record['pin'])!=frozen:
            raise AdoptionInvalid('RELIGHT_QUALIFICATION_SCOPE_STALE')
        return record
    record=evidence();retained=canonical_bytes(record);bodies=[]
    for label,ref,maximum in [('report',frozen.report,8<<20),('calibration',frozen.calibration,1<<20)]:
        location=record[label]
        if (not isinstance(location,dict) or set(location)!={'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'}
                or location['artifact_id']!=str(ref.artifact_id) or location['sha256']!=ref.sha256
                or location['bucket']!=bucket or location['media_type']!='application/json'
                or type(location['bytes']) is not int or not 2<=location['bytes']<=maximum
                or not isinstance(location['object_key'],str) or not location['object_key']
                or not isinstance(location['object_version'],str) or location['object_version'] in ('','null')):
            raise AdoptionInvalid('RELIGHT_QUALIFICATION_OBJECT_INVALID')
        check_active();response=objects.get_object(bucket,location['object_key'],version_id=location['object_version'])
        try:
            with closing(response):
                raw=bytearray()
                while len(raw)<=location['bytes']:
                    check_active();block=response.read(min(65536,location['bytes']+1-len(raw)))
                    if not isinstance(block,bytes):raise AdoptionInvalid('RELIGHT_QUALIFICATION_STREAM_INVALID')
                    if not block:break
                    raw.extend(block)
                if len(raw)!=location['bytes']:raise AdoptionInvalid('RELIGHT_QUALIFICATION_SIZE_CHANGED')
                bodies.append(bytes(raw))
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()
    verify_relight_qualification(record['pin'],*bodies,record['policy'],params=params,
        expected_runtime_sha=expected_runtime_sha,check_active=check_active)
    def current_pin():
        current=evidence()
        if canonical_bytes(current)!=retained:raise AdoptionInvalid('RELIGHT_QUALIFICATION_SCOPE_STALE')
        return current['pin']
    current_pin()
    return bodies[0],bodies[1],deepcopy(record['policy']),current_pin
