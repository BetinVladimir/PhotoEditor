"""Bounded scheduler snapshot reads for completed negative reconstruction."""
from copy import deepcopy
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json,ArtifactRef
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from .negative_qualification import NegativeQualificationPin
from .adoption import AdoptionInvalid


def load_negative_adoption_evidence(connection,tenant_id,task_id,context,params_raw,*,check_active):
    if connection.autocommit is not True or not callable(check_active):
        raise ValueError('NEGATIVE_ADOPTION_READ_CONFIGURATION')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise AdoptionInvalid('NEGATIVE_PARAMS_INVALID')
    params=OpticalNegativeParams.model_validate_json(params_raw)
    if canonical_bytes(params)!=params_raw:raise AdoptionInvalid('NEGATIVE_PARAMS_NONCANONICAL')
    ctx=deepcopy(context)
    check_active()
    row=connection.execute('SELECT photo.v29_negative_adoption_evidence(%s,%s,%s,%s,%s,%s)',
        (tenant_id,task_id,ctx.attempt,ctx.fence,ctx.result_id,params_raw.decode('utf-8'))).fetchone()
    check_active()
    if row is None or row[0] is None:raise AdoptionInvalid('NEGATIVE_ADOPTION_CONTEXT_STALE')
    raw=canonical_bytes(row[0]);value=parse_json(raw,1<<20)
    fields={'schema_version','run_id','task_id','attempt','fence','result_id','params_ref','params','profile','qualification_objects'}
    try:
        if (not isinstance(value,dict) or set(value)!=fields or type(value['schema_version']) is not int
                or value['schema_version']!=1 or value['task_id']!=str(task_id)
                or value['result_id']!=str(ctx.result_id) or type(value['attempt']) is not int
                or value['attempt']!=ctx.attempt or not 1<=value['attempt']<=6
                or type(value['fence']) is not int or value['fence']!=ctx.fence
                or not 1<=value['fence']<=2**53-1 or UUID(value['run_id']).int==0):raise ValueError()
        ref=ArtifactRef.model_validate_json(canonical_bytes(value['params_ref']))
        if ref.artifact_id.int==0 or ref.sha256!=sha256(params_raw).hexdigest() or canonical_bytes(value['params'])!=params_raw:raise ValueError()
        profile=value['profile']
        pin=NegativeQualificationPin.from_value(profile['negative_qualification'])
        if (profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2'
                or profile.get('negative_runtime_sha')!=pin.runtime_lock_sha
                or params.calibration.artifact_id!=pin.calibration.artifact_id
                or params.calibration.sha256!=pin.calibration.sha256):raise ValueError()
        objects=value['qualification_objects']
        if not isinstance(objects,list) or len(objects)!=2:raise ValueError()
        for item,contract,expected,limit in zip(objects,('NegativeCalibration','QualificationReport'),
                (pin.calibration,pin.report),(65536,8<<20)):
            if (not isinstance(item,dict) or set(item)!={'contract','artifact_id','sha256','bucket','object_key','object_version','bytes'}
                    or item['contract']!=contract or item['artifact_id']!=str(expected.artifact_id)
                    or item['sha256']!=expected.sha256 or type(item['bytes']) is not int
                    or not 2<=item['bytes']<=limit or any(not isinstance(item[key],str)
                    or not 1<=len(item[key])<=1024 for key in ('bucket','object_key','object_version'))
                    or item['object_version']=='null'):raise ValueError()
        if objects[0]['bucket']!=objects[1]['bucket']:raise ValueError()
    except (ValueError,TypeError,KeyError,AttributeError) as error:
        raise AdoptionInvalid('NEGATIVE_ADOPTION_CONTEXT_INVALID') from error
    check_active();return value


def negative_adoption_checkpoint(connection,tenant_id,task_id,context,params_raw,evidence,*,check_active):
    ctx=deepcopy(context);expected=canonical_bytes(evidence)
    def checkpoint():
        current=load_negative_adoption_evidence(connection,tenant_id,task_id,ctx,params_raw,check_active=check_active)
        if canonical_bytes(current)!=expected:raise AdoptionInvalid('NEGATIVE_ADOPTION_CONTEXT_CHANGED')
        return current
    return checkpoint
