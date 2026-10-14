"""Canonical-result admission callback; no completed-task lease reuse."""
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from .adoption import AdoptionInvalid
from .exposure_params import load_exposure_adoption_params


def sql_exposure_adoption_admission(connection,tenant_id,task_id,context,reader,*,check_active):
    context=deepcopy(context)
    params,edit=load_exposure_adoption_params(connection,tenant_id,task_id,context,reader,check_active=check_active)
    raw=reader.read_json(params,1048576)
    # Preserve original pinned bytes, not reserialized bytes, for SQL SHA proof.
    from hashlib import sha256
    if not isinstance(raw,bytes) or sha256(raw).hexdigest()!=params.sha256:
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
    expected=canonical_bytes(edit)
    def verify(value):
        check_active()
        value=ApprovedSemanticEdit.model_validate_json(canonical_bytes(value))
        if canonical_bytes(value)!=expected:return False
        row=connection.execute('SELECT photo.v27_exposure_adoption_ready(%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant_id,task_id,context.attempt,context.fence,context.result_id,
             params.artifact_id,params.sha256,raw.decode('utf-8'))).fetchone()
        check_active()
        return row is not None and row[0] is True
    return params,edit,verify
