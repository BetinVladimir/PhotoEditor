"""Short autocommit scheduler reads for completed donor reconstruction."""
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json,ArtifactRef
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.contracts.donor_confirmation import verify_open_eyes_confirmation
from .portrait_donor_qualification import donor_compiler_profile,DonorQualificationPin
from .adoption import AdoptionInvalid


def load_donor_adoption_evidence(connection,tenant_id,task_id,context,*,check_active):
    if connection.autocommit is not True or not callable(check_active):
        raise ValueError('DONOR_ADOPTION_READ_CONFIGURATION')
    check_active()
    row=connection.execute('SELECT photo.v28_donor_adoption_evidence(%s,%s,%s,%s,%s)',
        (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
    check_active()
    if row is None or row[0] is None:raise AdoptionInvalid('DONOR_ADOPTION_CONTEXT_STALE')
    raw=canonical_bytes(row[0])
    if len(raw)>1<<20:raise AdoptionInvalid('DONOR_ADOPTION_CONTEXT_INVALID')
    value=parse_json(raw,1<<20)
    fields={'schema_version','run_id','task_id','attempt','fence','result_id','params_ref',
            'params','proof_jcs','profile','manual','qualification_objects'}
    if (not isinstance(value,dict) or set(value)!=fields or type(value['schema_version']) is not int
            or value['schema_version']!=1 or value['task_id']!=str(task_id)
            or value['result_id']!=str(context.result_id) or type(value['attempt']) is not int
            or type(value['fence']) is not int or not 1<=value['attempt']<=6
            or not 1<=value['fence']<=2**53-1 or UUID(str(task_id)).int==0
            or UUID(str(context.result_id)).int==0 or value['attempt']!=context.attempt or value['fence']!=context.fence):
        raise AdoptionInvalid('DONOR_ADOPTION_CONTEXT_INVALID')
    try:
        if UUID(value['run_id']).int==0:raise ValueError()
        ref=ArtifactRef.model_validate_json(canonical_bytes(value['params_ref']))
        body=canonical_bytes(value['params'])
        params=PortraitOpenEyesParams.model_validate_json(body)
        if not 2<=len(body)<=65536 or sha256(body).hexdigest()!=ref.sha256 or ref.artifact_id.int==0:raise ValueError()
        proof=value['proof_jcs']
        if not isinstance(proof,str) or not 2<=len(proof.encode())<=65536:raise ValueError()
        verify_open_eyes_confirmation(params,proof.encode())
        profile=donor_compiler_profile(value['profile'])
        _validate_nested_evidence(value['manual'],value['qualification_objects'],
            DonorQualificationPin.from_value(profile['donor_qualification']),ref)
    except (ValueError,TypeError,KeyError) as exc:
        raise AdoptionInvalid('DONOR_ADOPTION_CONTEXT_INVALID') from exc
    check_active()
    return value


def _validate_nested_evidence(manual,objects,pin,params_ref):
    if not isinstance(manual,dict) or set(manual)!={'guard_pixel','original_params','root','target_pixel','vectors'}:
        raise ValueError('DONOR_MANUAL_EVIDENCE_INVALID')
    refs=[]
    for name in ('guard_pixel','original_params','root','target_pixel','vectors'):
        value=manual[name]
        if name=='vectors' and value is None:continue
        ref=ArtifactRef.model_validate_json(canonical_bytes(value))
        if ref.artifact_id.int==0:raise ValueError('DONOR_MANUAL_EVIDENCE_INVALID')
        refs.append(ref)
    ids=[ref.artifact_id for ref in refs]+[pin.report.artifact_id,pin.calibration.artifact_id,params_ref.artifact_id]
    if len(set(ids))!=len(ids):raise ValueError('DONOR_EVIDENCE_ALIAS')
    if not isinstance(objects,list) or len(objects)!=2:raise ValueError('DONOR_QUALIFICATION_OBJECT_INVALID')
    for item,contract,ref,maximum in zip(objects,('Calibration','Report'),(pin.calibration,pin.report),(1<<20,8<<20)):
        if (not isinstance(item,dict) or set(item)!={'contract','artifact_id','sha256','bucket','object_key','object_version','bytes'}
                or item['contract']!=contract or item['artifact_id']!=str(ref.artifact_id) or item['sha256']!=ref.sha256
                or type(item['bytes']) is not int or not 2<=item['bytes']<=maximum
                or any(not isinstance(item[name],str) or not item[name] for name in ('bucket','object_key','object_version'))
                or item['object_version']=='null'):
            raise ValueError('DONOR_QUALIFICATION_OBJECT_INVALID')
    if objects[0]['bucket']!=objects[1]['bucket']:raise ValueError('DONOR_QUALIFICATION_OBJECT_INVALID')


def donor_adoption_checkpoint(connection,tenant_id,task_id,context,evidence,*,check_active):
    """Repeat current completed-task evidence; changed snapshots are not grants."""
    from copy import deepcopy
    owned_context=deepcopy(context)
    expected=canonical_bytes(evidence)
    def checkpoint():
        current=load_donor_adoption_evidence(connection,tenant_id,task_id,owned_context,check_active=check_active)
        if canonical_bytes(current)!=expected:
            raise AdoptionInvalid('DONOR_ADOPTION_CONTEXT_CHANGED')
        return current
    return checkpoint
