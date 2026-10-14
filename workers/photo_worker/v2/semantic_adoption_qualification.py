"""Completed-result adapter for the shared exact-version qualification verifier."""
from copy import deepcopy
from types import SimpleNamespace
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import SemanticTransferParams
from .adoption import ArtifactRef,AdoptionInvalid
from .semantic_qualification_pin import SemanticQualificationPin
from .semantic_qualification_loader import semantic_qualification_loader


def semantic_adoption_qualification_loader(connection,tenant_id,task_id,context,params,objects,bucket,*,check_active=lambda:None):
    if not connection.autocommit or not callable(check_active):
        raise ValueError('SEMANTIC_ADOPTION_QUALIFICATION_CONFIGURATION_INVALID')
    context=deepcopy(context)
    params=SemanticTransferParams.model_validate_json(canonical_bytes(params))
    scope=(tenant_id,task_id,context.attempt,context.fence,context.result_id)
    def evidence(owner):
        if owner!=scope:raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
        check_active()
        row=connection.execute('SELECT photo.v27_semantic_adoption_report_evidence(%s,%s,%s,%s,%s)',scope).fetchone()
        check_active()
        return None if row is None else row[0]
    record=evidence(scope)
    if not isinstance(record,dict):raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
    pin=SemanticQualificationPin.from_value(record.get('pin'))
    pin.bind(params.model_lock_sha,ArtifactRef(params.calibration.artifact_id,params.calibration.sha256))
    return semantic_qualification_loader(SimpleNamespace(semantic_report_evidence=evidence),scope,objects,bucket,
        frozen_pin=pin,check_active=check_active)
