"""Own canonical-result SQL admission, no cached successful authorization."""
from hashlib import sha256
from uuid import UUID
from .adoption import ArtifactRef,AdoptionInvalid,require_adoption_active


def sql_retouch_preview_adoption_admission(connection,tenant_id,task_id,context,*,check_active):
    if not callable(getattr(connection,'execute',None)) or not callable(check_active):
        raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_CONTEXT_REQUIRED')
    scope=(tenant_id,task_id,context.attempt,context.fence,context.result_id)
    if (any(not isinstance(value,UUID) or value.int==0 for value in (tenant_id,task_id,context.result_id))
            or type(context.attempt) is not int or context.attempt<1
            or type(context.fence) is not int or context.fence<1
            or context.operation!='semantic.preview_retouch_region' or context.version!='1'):
        raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_CONTEXT_REQUIRED')
    statement='SELECT * FROM photo.v27_retouch_preview_adoption_params(%s,%s,%s,%s,%s)'
    def resolve():
        require_adoption_active(check_active)
        row=connection.execute(statement,scope).fetchone()
        require_adoption_active(check_active)
        if row is None or len(row)!=2:
            raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_PARAMS_STALE')
        return ArtifactRef.parse(dict(artifact_id=str(row[0]),sha256=row[1]))
    params=resolve()
    def admitted(raw):
        require_adoption_active(check_active)
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=params.sha256:
            return False
        return resolve()==params
    return params,admitted
