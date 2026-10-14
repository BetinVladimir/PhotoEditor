"""Repeated completed-task original manual producer authority for reconstruction."""
from copy import deepcopy
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from .adoption import AdoptionInvalid


def donor_adoption_manual_authority(connection,tenant_id,task_id,context,evidence,*,checkpoint,check_active):
    if connection.autocommit is not True or not all(callable(c) for c in (checkpoint,check_active)):
        raise ValueError('DONOR_ADOPTION_READ_CONFIGURATION')
    owned=parse_json(canonical_bytes(evidence),1<<20);ctx=deepcopy(context)
    manual=owned['manual'];body=canonical_bytes(owned['params'])
    def ref(name):return ArtifactRef.model_validate_json(canonical_bytes(manual[name]))
    root,producer,target,guard=[ref(name) for name in ('root','original_params','target_pixel','guard_pixel')]
    vector=None if manual['vectors'] is None else ref('vectors')
    expected=canonical_bytes(owned)
    def current():
        check_active()
        if canonical_bytes(checkpoint())!=expected:raise AdoptionInvalid('DONOR_ADOPTION_CONTEXT_CHANGED')
        check_active()
    def verify(params,root_ref,producer_ref):
        current()
        if canonical_bytes(params)!=body or root_ref!=root or producer_ref!=producer:
            raise AdoptionInvalid('DONOR_ADOPTION_MANUAL_CHANGED')
        row=connection.execute('SELECT * FROM photo.v28_donor_adoption_manual_origin('+','.join(['%s']*11)+')',
            (tenant_id,task_id,ctx.attempt,ctx.fence,ctx.result_id,target.artifact_id,target.sha256,
             guard.artifact_id,guard.sha256,None if vector is None else vector.artifact_id,
             None if vector is None else vector.sha256)).fetchone()
        check_active()
        if (not isinstance(row,(tuple,list)) or len(row)!=4
                or ArtifactRef(artifact_id=row[0],sha256=row[1])!=root
                or ArtifactRef(artifact_id=row[2],sha256=row[3])!=producer):
            raise AdoptionInvalid('DONOR_ADOPTION_MANUAL_STALE')
        current()
        return True
    return verify
