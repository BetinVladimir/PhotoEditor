"""Exact-version CPU report proof from canonical-result SQL evidence."""
from copy import deepcopy
from types import SimpleNamespace
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid
from .exposure_params import load_exposure_adoption_params
from .exposure_qualification_pin import ExposureQualificationPin
from .semantic_qualification_report import verify_exposure_qualification_report
from .semantic_qualification_loader import _qualification_loader


def exposure_adoption_qualification_loader(connection,tenant_id,task_id,context,reader,
        objects,bucket,*,check_active):
    context=deepcopy(context)
    params,edit=load_exposure_adoption_params(connection,tenant_id,task_id,context,reader,check_active=check_active)
    raw=reader.read_json(params,1048576)
    from hashlib import sha256
    if not isinstance(raw,bytes) or sha256(raw).hexdigest()!=params.sha256:
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
    scope=(tenant_id,task_id,context.attempt,context.fence,context.result_id,
        params.artifact_id,params.sha256,raw.decode())
    def evidence(owner):
        if owner!=scope:raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
        check_active()
        row=connection.execute('SELECT photo.v27_exposure_adoption_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s)',scope).fetchone()
        check_active();return None if row is None else row[0]
    record=evidence(scope)
    if not isinstance(record,dict):raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
    pin=ExposureQualificationPin.from_value(record.get('pin'))
    return _qualification_loader(SimpleNamespace(semantic_report_evidence=evidence),scope,objects,
        bucket,frozen_pin=pin,pin_type=ExposureQualificationPin,
        verify_report=verify_exposure_qualification_report,check_active=check_active)


def exposure_worker_qualification_loader(database, lease, params, objects, bucket,
        *, frozen_pin, check_active=lambda: None):
    """Prove a CPU report against the original Params and live worker lease."""
    from hashlib import sha256
    if (not isinstance(params, bytes) or not 2 <= len(params) <= 1048576
            or sha256(params).hexdigest() != lease.command.params.sha256):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
    owned_params = bytes(params)
    def evidence(owner):
        return database.exposure_report_evidence(owner, owned_params)
    return _qualification_loader(SimpleNamespace(semantic_report_evidence=evidence),
        lease, objects, bucket, frozen_pin=frozen_pin,
        pin_type=ExposureQualificationPin,
        verify_report=verify_exposure_qualification_report, check_active=check_active)
