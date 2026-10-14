"""Live leased SQL evidence plus exact-version report bytes outside transactions."""
from contextlib import closing
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes,parse_json
from .adoption import AdoptionInvalid
from .semantic_qualification_pin import SemanticQualificationPin
from .semantic_qualification_report import verify_semantic_qualification_report


def _qualification_loader(database,lease,objects,allowed_bucket,*,frozen_pin,pin_type,verify_report,check_active):
    if (type(frozen_pin) is not pin_type or not isinstance(allowed_bucket,str)
            or not allowed_bucket or not callable(check_active)):
        raise ValueError('SEMANTIC_QUALIFICATION_LOADER_INVALID')
    owned_lease=deepcopy(lease)
    def evidence():
        check_active()
        record=parse_json(canonical_bytes(database.semantic_report_evidence(deepcopy(owned_lease))),8*1024*1024)
        check_active()
        if not isinstance(record,dict) or set(record)!= {'pin','policy','report'}:
            raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
        pin=pin_type.from_value(record['pin'])
        if pin!=frozen_pin:raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
        return record,pin
    def load():
        record,pin=evidence();location=record['report']
        keys={'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'}
        if (not isinstance(location,dict) or set(location)!=keys
                or location['artifact_id']!=str(pin.report.artifact_id) or location['sha256']!=pin.report.sha256
                or location['bucket']!=allowed_bucket or location['media_type']!='application/json'
                or type(location['bytes']) is not int or not 2<=location['bytes']<=8*1024*1024
                or not isinstance(location['object_key'],str) or not location['object_key']
                or not isinstance(location['object_version'],str) or location['object_version'] in ('','null')):
            raise AdoptionInvalid('SEMANTIC_QUALIFICATION_OBJECT_INVALID')
        check_active()
        response=objects.get_object(allowed_bucket,location['object_key'],version_id=location['object_version'])
        try:
            with closing(response):
                body=bytearray()
                while len(body)<=location['bytes']:
                    check_active()
                    block=response.read(min(65536,location['bytes']+1-len(body)))
                    if not isinstance(block,bytes):raise AdoptionInvalid('SEMANTIC_QUALIFICATION_STREAM_INVALID')
                    if not block:break
                    body.extend(block)
                if len(body)!=location['bytes']:raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SIZE_CHANGED')
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()
        check_active()
        verify_report(bytes(body),pin,record['policy'],check_active=check_active)
        after,_=evidence()
        if canonical_bytes(after)!=canonical_bytes(record):raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
        return record['pin']
    return load


def semantic_qualification_loader(database,lease,objects,allowed_bucket,*,frozen_pin,check_active=lambda:None):
    return _qualification_loader(database,lease,objects,allowed_bucket,frozen_pin=frozen_pin,
        pin_type=SemanticQualificationPin,verify_report=verify_semantic_qualification_report,
        check_active=check_active)
