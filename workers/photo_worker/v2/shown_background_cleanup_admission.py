"""Result-scoped receipt admission using original pinned Params bytes."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.semantic_preview import ShownBackgroundCleanupAdoptionInput
from .adoption import ArtifactRef,AdoptionInvalid,require_adoption_active


def sql_shown_background_cleanup_adoption_admission(connection,tenant_id,task_id,context,reader,*,check_active):
    owner=deepcopy(context)
    def active():require_adoption_active(check_active)
    active()
    row=connection.execute('SELECT * FROM photo.v27_shown_background_cleanup_adoption_params(%s,%s,%s,%s,%s)',
        (tenant_id,task_id,owner.attempt,owner.fence,owner.result_id)).fetchone()
    active()
    if row is None:raise AdoptionInvalid('SEMANTIC_SHOWN_PARAMS_STALE')
    ref=ArtifactRef(row[0],row[1]);e=reader.evidence(ref);active()
    if (e.ref!=ref or e.source!='input' or e.media_type!='application/json'
            or type(e.bytes) is not int or not 2<=e.bytes<=1048576):
        raise AdoptionInvalid('SEMANTIC_SHOWN_EVIDENCE_INVALID')
    raw=reader.read_json(ref,1048576);active()
    if (not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or len(raw)!=e.bytes
            or sha256(raw).hexdigest()!=ref.sha256):
        raise AdoptionInvalid('SEMANTIC_SHOWN_BYTES_INVALID')
    try:params=ShownBackgroundCleanupAdoptionInput.model_validate_json(canonical_bytes(parse_json(raw,max_bytes=1048576)))
    except ValueError:raise AdoptionInvalid('SEMANTIC_SHOWN_CONTRACT_INVALID') from None
    if params.tenant_id!=tenant_id:raise AdoptionInvalid('SEMANTIC_SHOWN_SCOPE_INVALID')
    def verify(value):
        active()
        if not isinstance(value,bytes) or value!=raw:return False
        row=connection.execute('SELECT photo.v27_shown_background_cleanup_adoption_ready(%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant_id,task_id,owner.attempt,owner.fence,owner.result_id,ref.artifact_id,ref.sha256,raw.decode('utf-8'))).fetchone()
        active();return row is not None and row[0] is True
    if verify(raw) is not True:raise AdoptionInvalid('SEMANTIC_SHOWN_ADMISSION_DENIED')
    return ref,verify
