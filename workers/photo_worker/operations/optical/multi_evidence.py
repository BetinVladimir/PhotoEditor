"""Bounded original qualification object reads behind current leased authority."""
from contextlib import closing
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
from photo_worker.v2.negative_qualification import NegativeQualificationPin
from photo_worker.v2.adoption import AdoptionInvalid
from .multi_admission import MultiImageEvidence


def load_multi_evidence(database,lease,params_raw,objects,bucket,*,check_active):
    """Database resolver must enforce live tenant/project/lease/catalog authority.

    No SQL method is inferred from a generic artifact locator. Missing dedicated
    authority is an installation error, never permission to use object bytes.
    """
    if (not callable(check_active) or not callable(getattr(database,'multi_qualification_evidence',None)) or
        not callable(getattr(objects,'get_object',None)) or not isinstance(bucket,str) or not 1<=len(bucket)<=255):
        raise ValueError('MULTI_AUTHORITY_INVALID')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('MULTI_PARAMS_INVALID')
    owned=deepcopy(lease);body=bytes(params_raw);command=owned.command
    if (command.operation not in ('optical.panorama','optical.focus_stack') or command.operation_version!='1' or command.resource_class!='cpu' or
        sha256(body).hexdigest()!=command.params.sha256):raise ValueError('MULTI_PARAMS_INVALID')
    model=OpticalPanoramaParams if command.operation=='optical.panorama' else OpticalFocusStackParams
    params=model.model_validate_json(body)
    if canonical_bytes(params)!=body:raise ValueError('MULTI_PARAMS_INVALID')
    profile=parse_json(canonical_bytes(owned.execution_profile),1<<20)
    frozen=NegativeQualificationPin.from_value(profile.get('multi_qualification'))
    if (profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2' or
        profile.get('multi_runtime_sha')!=frozen.runtime_lock_sha):
        raise AdoptionInvalid('MULTI_QUALIFICATION_PROFILE_INVALID')
    def evidence():
        check_active()
        record=parse_json(canonical_bytes(database.multi_qualification_evidence(deepcopy(owned),body)),8<<20)
        check_active()
        if (not isinstance(record,dict) or set(record)!={'pin','policy','report','calibration'} or
            NegativeQualificationPin.from_value(record['pin'])!=frozen):
            raise AdoptionInvalid('MULTI_QUALIFICATION_SCOPE_STALE')
        return record
    record=evidence();retained=canonical_bytes(record)
    def current():
        if canonical_bytes(evidence())!=retained:raise AdoptionInvalid('MULTI_QUALIFICATION_SCOPE_STALE')
    specifications=(('report',frozen.report,8<<20),('calibration',frozen.calibration,1<<20))
    for label,ref,maximum in specifications:
        location=record[label]
        if (not isinstance(location,dict) or set(location)!={'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'} or
            location['artifact_id']!=str(ref.artifact_id) or location['sha256']!=ref.sha256 or location['bucket']!=bucket or
            location['media_type']!='application/json' or type(location['bytes']) is not int or not 2<=location['bytes']<=maximum or
            not isinstance(location['object_key'],str) or not 1<=len(location['object_key'])<=1024 or
            not isinstance(location['object_version'],str) or not 1<=len(location['object_version'])<=1024 or location['object_version']=='null'):
            raise AdoptionInvalid('MULTI_QUALIFICATION_OBJECT_INVALID')
    bodies={}
    for label,ref,maximum in specifications:
        location=record[label];current();check_active()
        response=objects.get_object(bucket,location['object_key'],version_id=location['object_version'])
        try:
            with closing(response):
                raw=bytearray()
                while len(raw)<=location['bytes']:
                    check_active();limit=min(65536,location['bytes']+1-len(raw));block=response.read(limit)
                    if not isinstance(block,bytes) or len(block)>limit:raise AdoptionInvalid('MULTI_QUALIFICATION_STREAM_INVALID')
                    if not block:break
                    raw.extend(block)
                if len(raw)!=location['bytes']:raise AdoptionInvalid('MULTI_QUALIFICATION_SIZE_CHANGED')
                original=bytes(raw)
                if sha256(original).hexdigest()!=ref.sha256 or canonical_bytes(parse_json(original,maximum))!=original:
                    raise AdoptionInvalid('MULTI_QUALIFICATION_BYTES_INVALID')
                bodies[label]=original
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()
        current()
    current()
    return MultiImageEvidence(deepcopy(record['pin']),deepcopy(record['policy']),bodies['report'],bodies['calibration'],
        command.operation,body)


def multi_evidence_resolver(database,objects,bucket):
    """Create a per-admission loader; no successful live grant is cached."""
    def resolve(ctx,params):
        return load_multi_evidence(database,ctx.lease,canonical_bytes(params),objects,bucket,check_active=ctx.check_active)
    return resolve
