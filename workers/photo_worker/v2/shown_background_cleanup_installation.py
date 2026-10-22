"""Explicit result-scoped shown-copy installation with no report fallback."""
from copy import deepcopy
from dataclasses import dataclass
from .adoption import AdoptionInvalid,require_adoption_active
from .shown_background_cleanup_admission import sql_shown_background_cleanup_adoption_admission


@dataclass(frozen=True)
class ShownBackgroundCleanupAdoptionInstallation:
    params: object
    verify_admission: object


def sql_shown_background_cleanup_adoption_installation(connection,*,verify_qualification,check_active):
    if not connection.autocommit or not callable(verify_qualification) or not callable(check_active):
        raise ValueError('SEMANTIC_SHOWN_INSTALLATION_REQUIRED')
    def install(tenant_id,task_id,context,reader):
        owner=deepcopy(context)
        def active():require_adoption_active(check_active)
        active()
        params,sql_admission=sql_shown_background_cleanup_adoption_admission(connection,
            tenant_id,task_id,owner,reader,check_active=active)
        def admission(raw):
            active()
            qualified=verify_qualification(tenant_id,task_id,deepcopy(owner),reader,raw)
            active()
            if qualified is not True:return False
            accepted=sql_admission(raw);active();return accepted is True
        return ShownBackgroundCleanupAdoptionInstallation(params,admission)
    return install


def versioned_shown_background_cleanup_adoption_installation(connection,objects,bucket,*,check_active):
    """Validate own-operation report bytes with SQL metadata before/after IO."""
    from hashlib import sha256
    from types import SimpleNamespace
    from photo_worker.contracts.semantic_preview import ShownBackgroundCleanupAdoptionInput
    from .shown_background_cleanup_qualification import ShownBackgroundCleanupQualificationPin,verify_shown_background_cleanup_qualification_report
    from .semantic_qualification_loader import _qualification_loader
    def qualification(tenant_id,task_id,context,reader,raw):
        require_adoption_active(check_active)
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576:return False
        params=ShownBackgroundCleanupAdoptionInput.model_validate_json(raw)
        if params.tenant_id!=tenant_id:return False
        row=connection.execute('SELECT * FROM photo.v27_shown_background_cleanup_adoption_params(%s,%s,%s,%s,%s)',
            (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
        require_adoption_active(check_active)
        if row is None or sha256(raw).hexdigest()!=row[1]:return False
        scope=(tenant_id,task_id,context.attempt,context.fence,context.result_id,row[0],row[1],raw.decode('utf-8'))
        def evidence(owner):
            require_adoption_active(check_active)
            if owner!=scope:raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
            record=connection.execute('SELECT photo.v27_shown_background_cleanup_adoption_report_evidence('+','.join(['%s']*8)+')',scope).fetchone()
            require_adoption_active(check_active)
            return None if record is None else record[0]
        record=evidence(scope)
        if not isinstance(record,dict):return False
        pin=ShownBackgroundCleanupQualificationPin.from_value(record.get('pin'))
        if (pin.model_lock_sha,pin.runtime_lock_sha,pin.device_profile_sha)!=(
                params.model_lock_sha,params.runtime_lock_sha,params.device_profile_sha):return False
        load=_qualification_loader(SimpleNamespace(semantic_report_evidence=evidence),scope,objects,bucket,
            frozen_pin=pin,pin_type=ShownBackgroundCleanupQualificationPin,
            verify_report=verify_shown_background_cleanup_qualification_report,check_active=check_active)
        load();require_adoption_active(check_active);return True
    return sql_shown_background_cleanup_adoption_installation(connection,
        verify_qualification=qualification,check_active=check_active)
