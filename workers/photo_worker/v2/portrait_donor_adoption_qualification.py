"""Original completed-task donor qualification reads outside SQL transactions."""
from contextlib import closing
from photo_worker.contracts.base import canonical_bytes,parse_json
from .portrait_donor_qualification import DonorQualificationPin,donor_compiler_profile,verify_donor_qualification
from .adoption import AdoptionInvalid


def load_donor_adoption_qualification(evidence,objects,policy,*,bucket,checkpoint,check_active):
    """Caller supplies validated scheduler evidence and trusted policy.

    Snapshot authority repeats before/after original object IO. Returned
    Calibration is content verified; no adoption permission is granted here.
    """
    if not callable(checkpoint) or not callable(check_active) or not isinstance(bucket,str) or not bucket:
        raise ValueError('DONOR_AUTHORITY')
    expected=canonical_bytes(evidence)
    if len(expected)>1<<20:raise ValueError('DONOR_ADOPTION_CONTEXT_INVALID')
    owned=parse_json(expected,1<<20);owned_policy=parse_json(canonical_bytes(policy),1<<20)
    profile=donor_compiler_profile(owned['profile']);pin=DonorQualificationPin.from_value(profile['donor_qualification'])
    locations=owned['qualification_objects']
    if not isinstance(locations,list) or len(locations)!=2:raise AdoptionInvalid('DONOR_QUALIFICATION_OBJECT_INVALID')
    def current():
        check_active()
        if canonical_bytes(checkpoint())!=expected:raise AdoptionInvalid('DONOR_ADOPTION_CONTEXT_CHANGED')
        check_active()
    raws=[]
    for location,contract,ref,maximum in zip(locations,('Calibration','Report'),(pin.calibration,pin.report),(1<<20,8<<20)):
        if (not isinstance(location,dict) or set(location)!={'contract','artifact_id','sha256','bucket','object_key','object_version','bytes'}
                or location['contract']!=contract or location['artifact_id']!=str(ref.artifact_id)
                or location['sha256']!=ref.sha256 or location['bucket']!=bucket
                or type(location['bytes']) is not int or not 2<=location['bytes']<=maximum
                or any(not isinstance(location[key],str) or not location[key] for key in ('object_key','object_version'))
                or location['object_version']=='null'):
            raise AdoptionInvalid('DONOR_QUALIFICATION_OBJECT_INVALID')
    current()
    for location in locations:
        current();response=objects.get_object(bucket,location['object_key'],version_id=location['object_version'])
        try:
            with closing(response):
                raw=bytearray()
                while len(raw)<=location['bytes']:
                    check_active();limit=min(65536,location['bytes']+1-len(raw));block=response.read(limit)
                    if not isinstance(block,bytes) or len(block)>limit:raise AdoptionInvalid('DONOR_QUALIFICATION_STREAM_INVALID')
                    if not block:break
                    raw.extend(block)
                if len(raw)!=location['bytes']:raise AdoptionInvalid('DONOR_QUALIFICATION_SIZE_CHANGED')
                raws.append(bytes(raw))
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()
        current()
    verified,calibration=verify_donor_qualification(profile,raws[1],raws[0],owned_policy,check_active=check_active)
    current()
    return verified,calibration
