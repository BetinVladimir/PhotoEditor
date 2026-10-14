"""Completed relight source/qualification metadata, not producer or commit proof."""
from copy import deepcopy
from uuid import UUID
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.contracts.relight import ImageRelightParams
from .relight_qualification import RelightQualificationPin
from .adoption import AdoptionInvalid


def load_relight_adoption_source(connection,tenant_id,task_id,context,params_raw,*,check_active):
    if connection.autocommit is not True or not callable(check_active):
        raise ValueError('RELIGHT_ADOPTION_READ_CONFIGURATION')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise AdoptionInvalid('RELIGHT_ADOPTION_PARAMS_INVALID')
    params=ImageRelightParams.model_validate_json(params_raw)
    if canonical_bytes(params)!=params_raw:raise AdoptionInvalid('RELIGHT_ADOPTION_PARAMS_INVALID')
    ctx=deepcopy(context);check_active()
    row=connection.execute('SELECT photo.v28_relight_adoption_source(%s,%s,%s,%s,%s,%s)',
        (tenant_id,task_id,ctx.attempt,ctx.fence,ctx.result_id,params_raw.decode('utf-8'))).fetchone()
    check_active()
    if row is None or row[0] is None:raise AdoptionInvalid('RELIGHT_ADOPTION_SOURCE_STALE')
    raw=canonical_bytes(row[0]);value=parse_json(raw,1<<20)
    fields={'schema_version','run_id','task_id','attempt','fence','result_id','params_ref','root_ref','profile','qualification'}
    if (not isinstance(value,dict) or set(value)!=fields or type(value['schema_version']) is not int
            or value['schema_version']!=1 or value['task_id']!=str(task_id) or value['result_id']!=str(ctx.result_id)
            or type(value['attempt']) is not int or value['attempt']!=ctx.attempt or value['attempt']<1
            or type(value['fence']) is not int or value['fence']!=ctx.fence or not 1<=value['fence']<=2**53-1):
        raise AdoptionInvalid('RELIGHT_ADOPTION_SOURCE_INVALID')
    try:
        if UUID(value['run_id']).int==0:raise ValueError()
        params_ref=ArtifactRef.model_validate_json(canonical_bytes(value['params_ref']))
        root=ArtifactRef.model_validate_json(canonical_bytes(value['root_ref']))
        profile=value['profile'];pin=RelightQualificationPin.from_value(value['qualification'])
        if (params_ref.artifact_id.int==0 or params_ref.sha256!=sha256(params_raw).hexdigest()
                or root.artifact_id!=ctx.root.artifact_id or root.sha256!=ctx.root.sha256
                or profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2'
                or profile.get('relight_runtime_sha')!=pin.runtime_lock_sha
                or RelightQualificationPin.from_value(profile.get('relight_qualification'))!=pin
                or pin.calibration.artifact_id!=params.calibration.artifact_id
                or pin.calibration.sha256!=params.calibration.sha256):raise ValueError()
    except (ValueError,TypeError,KeyError,AttributeError) as exc:
        raise AdoptionInvalid('RELIGHT_ADOPTION_SOURCE_INVALID') from exc
    check_active();return value


def relight_adoption_source_checkpoint(connection,tenant_id,task_id,context,params_raw,evidence,*,check_active):
    ctx=deepcopy(context);body=bytes(params_raw);expected=canonical_bytes(evidence)
    def checkpoint():
        current=load_relight_adoption_source(connection,tenant_id,task_id,ctx,body,check_active=check_active)
        if canonical_bytes(current)!=expected:raise AdoptionInvalid('RELIGHT_ADOPTION_SOURCE_CHANGED')
        return current
    return checkpoint
