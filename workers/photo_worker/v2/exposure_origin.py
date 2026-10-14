"""Install exact-source canonical-result protection origin for adoption."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from .adoption import AdoptionInvalid
from .exposure_params import load_exposure_adoption_params


def sql_exposure_adoption_origin(connection,tenant_id,task_id,context,reader,*,check_active):
    context=deepcopy(context)
    params,edit=load_exposure_adoption_params(connection,tenant_id,task_id,context,reader,check_active=check_active)
    raw=reader.read_json(params,1048576)
    if not isinstance(raw,bytes) or sha256(raw).hexdigest()!=params.sha256:
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
    source_bytes=canonical_bytes(edit.source)
    def verify(binding,root,binary):
        check_active()
        if canonical_bytes(binding)!=source_bytes:return False
        root=ArtifactRef.model_validate_json(canonical_bytes(root))
        binary=ArtifactRef.model_validate_json(canonical_bytes(binary))
        if root.artifact_id.int==0 or binary.artifact_id.int==0:return False
        row=connection.execute('SELECT photo.v27_exposure_adoption_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant_id,task_id,context.attempt,context.fence,context.result_id,
             params.artifact_id,params.sha256,raw.decode('utf-8'),root.artifact_id,
             root.sha256,binary.artifact_id,binary.sha256)).fetchone()
        check_active()
        return row is not None and row[0] is True
    return verify
