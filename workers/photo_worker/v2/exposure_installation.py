"""Explicit SQL-backed exposure adoption installation; no qualification fallback."""
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import ColorSpace
from .adoption import AdoptionInvalid
from .exposure_adoption import ExposureAdoptionInstallation
from .exposure_admission import sql_exposure_adoption_admission
from .exposure_origin import sql_exposure_adoption_origin
from .exposure_protection_pin import load_exposure_protection_pin


def sql_exposure_adoption_installation(connection, *, working_icc_sha,
        verify_qualification, check_active):
    """Qualification callback must validate actual pinned report/runtime evidence.

    SQL metadata admission alone is insufficient to qualify a backend. Factory
    receives the coordinator's result-scoped reader and immutable context.
    """
    if (not connection.autocommit or not callable(verify_qualification)
            or not callable(check_active) or not isinstance(working_icc_sha,str)
            or len(working_icc_sha)!=64 or any(c not in '0123456789abcdef' for c in working_icc_sha)):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    def install(tenant_id,task_id,context,reader):
        context=deepcopy(context);check_active()
        params,edit,sql_admission=sql_exposure_adoption_admission(connection,
            tenant_id,task_id,context,reader,check_active=check_active)
        def admission(value):
            check_active()
            if verify_qualification(tenant_id,task_id,deepcopy(context),reader,
                    value.model_copy(deep=True)) is not True:return False
            check_active()
            return sql_admission(value)
        if admission(edit) is not True:raise AdoptionInvalid('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
        protection,mask=load_exposure_protection_pin(connection,tenant_id,task_id,
            context,params,edit,reader,check_active=check_active)
        origin=sql_exposure_adoption_origin(connection,tenant_id,task_id,context,reader,check_active=check_active)
        if origin(edit.source.model_copy(deep=True),protection.model_copy(deep=True),
                  mask.artifact.model_copy(deep=True)) is not True:
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_PROTECTION_ORIGIN_STALE')
        def color(value):
            check_active();value=ColorSpace.model_validate_json(canonical_bytes(value))
            valid=(value.profile_id=='work_rec2020_gamma24_v1' and value.encoding=='gamma24'
                and value.icc_sha256==working_icc_sha)
            check_active();return valid
        check_active()
        return ExposureAdoptionInstallation(params,protection,admission,origin,color)
    return install


def versioned_exposure_adoption_installation(connection,objects,bucket,*,working_icc_sha,check_active):
    """Install actual SQL-scoped exact-version report verification on every call."""
    from .exposure_qualification_loader import exposure_adoption_qualification_loader
    def qualification(tenant_id,task_id,context,reader,edit):
        load=exposure_adoption_qualification_loader(connection,tenant_id,task_id,context,
            reader,objects,bucket,check_active=check_active)
        load()
        check_active()
        return True
    return sql_exposure_adoption_installation(connection,working_icc_sha=working_icc_sha,
        verify_qualification=qualification,check_active=check_active)
