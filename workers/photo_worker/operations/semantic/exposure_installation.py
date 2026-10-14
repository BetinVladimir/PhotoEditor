"""Compose leased report-byte proof with measured CPU runtime and locked ICC."""
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.exposure_qualification_pin import ExposureQualificationPin
from photo_worker.v2.semantic_qualification_pin import _sha
from photo_worker.v2.exposure_qualification_loader import exposure_worker_qualification_loader
from .exposure_adapter import locked_local_exposure_installation


def sql_local_exposure_factory(*, database, objects, bucket, icc_registry,
        resolve_protection=None, model_lock_sha, runtime_lock_sha, device_profile_sha, dataset_sha):
    """Installer measurements are trusted deployment inputs, never task overrides.

    No operation is registered by this factory. Protection discovery uses leased SQL by default; execution independently
    verifies the discovered root and binary SQL producer origin.
    """
    expected = (model_lock_sha, runtime_lock_sha, device_profile_sha, dataset_sha)
    if (any(not _sha(value) for value in expected)
            or any(not callable(getattr(database, name, None)) for name in
                ('exposure_report_evidence', 'admit_semantic_local_exposure',
                 'local_exposure_protection_origin'))
            or not callable(getattr(objects, 'get_object', None))
            or (resolve_protection is not None and not callable(resolve_protection))
            or (resolve_protection is None and not callable(getattr(database, 'exposure_protection_pin', None)))
            or not isinstance(bucket, str) or not 3 <= len(bucket) <= 63):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    def install(ctx, artifacts):
        lease = deepcopy(ctx.lease)
        def active():
            ctx.check_active()
            if ctx.lease != lease:
                raise AdoptionInvalid('SEMANTIC_LEASE_CHANGED')
        active()
        profile = deepcopy(lease.execution_profile)
        command = lease.command
        if (command.operation != 'semantic.local_exposure' or command.operation_version != '1'
                or command.resource_class != 'cpu' or profile.get('backend') != 'cpu'
                or profile.get('renderer_version') != 'v2.2'):
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_TASK_INVALID')
        pin = ExposureQualificationPin.from_value(profile.get('qualification'))
        if (pin.model_lock_sha, pin.runtime_lock_sha, pin.device_profile_sha, pin.dataset_sha) != expected:
            raise AdoptionInvalid('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')
        raw = artifacts.contract_bytes(command.params, ApprovedSemanticEdit)
        edit = ApprovedSemanticEdit.model_validate_json(raw)
        # Execution admission uses canonical Params; preserve that exact binding.
        if edit.kind != 'local_exposure' or canonical_bytes(edit) != raw:
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
        report = exposure_worker_qualification_loader(database, lease, raw, objects,
            bucket, frozen_pin=pin, check_active=active)
        def verify_runtime(value, supplied_profile):
            active()
            if canonical_bytes(value) != raw or canonical_bytes(supplied_profile) != canonical_bytes(profile):
                raise AdoptionInvalid('SEMANTIC_EXPOSURE_SCOPE_CHANGED')
            report()
            active()
            return True
        def protection(value):
            active()
            if canonical_bytes(value) != raw:
                raise AdoptionInvalid('SEMANTIC_EXPOSURE_SCOPE_CHANGED')
            result = (database.exposure_protection_pin(deepcopy(lease), raw)
                if resolve_protection is None else resolve_protection(value.model_copy(deep=True)))
            active()
            from photo_worker.contracts.base import ArtifactRef
            if result is None:
                raise AdoptionInvalid('SEMANTIC_EXPOSURE_PROTECTION_ORIGIN_STALE')
            return ArtifactRef.model_validate_json(canonical_bytes(result))
        installation = locked_local_exposure_installation(database=database,
            resolve_protection=protection, verify_runtime=verify_runtime,
            icc_registry=icc_registry)
        verify_runtime(edit, profile)
        return installation
    return install
