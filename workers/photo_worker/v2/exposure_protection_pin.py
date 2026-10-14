"""Resolve only the original producer's frozen protection document."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes,parse_json
from photo_worker.contracts.images import MaskRef
from .adoption import ArtifactRef,AdoptionInvalid


def load_exposure_protection_pin(connection,tenant_id,task_id,context,params,edit,reader,*,check_active):
    if not connection.autocommit or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    context=deepcopy(context);check_active()
    raw=reader.read_json(params,1048576)
    if not isinstance(raw,bytes) or sha256(raw).hexdigest()!=params.sha256:
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
    if canonical_bytes(parse_json(raw))!=canonical_bytes(edit):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_PARAMS_CHANGED')
    row=connection.execute('SELECT * FROM photo.v27_exposure_adoption_protection_pin(%s,%s,%s,%s,%s,%s,%s,%s)',
        (tenant_id,task_id,context.attempt,context.fence,context.result_id,params.artifact_id,params.sha256,raw.decode())).fetchone()
    check_active()
    if row is None:raise AdoptionInvalid('SEMANTIC_EXPOSURE_PROTECTION_STALE')
    pin=ContractRef(artifact_id=row[0],sha256=row[1])
    if pin.artifact_id.int==0:raise AdoptionInvalid('SEMANTIC_EXPOSURE_PROTECTION_INVALID')
    ref=ArtifactRef(pin.artifact_id,pin.sha256);evidence=reader.evidence(ref)
    if evidence.ref!=ref or evidence.source!='input' or evidence.media_type!='application/json':
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_EVIDENCE_INVALID')
    body=reader.read_json(ref,1048576);check_active()
    if (not isinstance(body,bytes) or len(body)!=evidence.bytes or not 2<=len(body)<=1048576
            or sha256(body).hexdigest()!=pin.sha256):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
    try:mask=MaskRef.model_validate_json(canonical_bytes(parse_json(body)))
    except ValueError as error:raise AdoptionInvalid('SEMANTIC_EXPOSURE_PROTECTION_INVALID') from error
    if not mask.protected or (mask.model_lock_id is None and mask.selector!='manual') or mask.artifact.artifact_id.int==0:
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_PROTECTION_INVALID')
    return pin,mask
