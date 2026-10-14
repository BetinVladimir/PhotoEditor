"""Result-scoped immutable approved Params reader for scheduler installation."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from .adoption import ArtifactRef,AdoptionInvalid


def load_exposure_adoption_params(connection,tenant_id,task_id,context,reader,*,check_active):
    if not connection.autocommit or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    context=deepcopy(context)
    if context.operation!='semantic.local_exposure' or context.version!='1':
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_TASK_CHANGED')
    check_active()
    row=connection.execute('SELECT * FROM photo.v27_exposure_adoption_params(%s,%s,%s,%s,%s)',
        (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
    check_active()
    if row is None:raise AdoptionInvalid('SEMANTIC_EXPOSURE_PARAMS_STALE')
    ref=ArtifactRef(row[0],row[1]);evidence=reader.evidence(ref)
    if evidence.ref!=ref or evidence.source!='input' or evidence.media_type!='application/json':
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_EVIDENCE_INVALID')
    raw=reader.read_json(ref,1048576);check_active()
    if (not isinstance(raw,bytes) or not 2<=len(raw)<=1048576
            or len(raw)!=evidence.bytes or sha256(raw).hexdigest()!=ref.sha256):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
    try:edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(parse_json(raw)))
    except ValueError as error:raise AdoptionInvalid('SEMANTIC_EXPOSURE_PARAMS_INVALID') from error
    if edit.kind!='local_exposure':raise AdoptionInvalid('SEMANTIC_EXPOSURE_PARAMS_INVALID')
    return ref,edit
