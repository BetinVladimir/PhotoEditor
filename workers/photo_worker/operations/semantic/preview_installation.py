"""Explicit preview SQL/qualified-runtime installer; does not register operations."""
from copy import deepcopy
from types import SimpleNamespace
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.exposure_qualification_pin import ExposureQualificationPin
from photo_worker.v2.semantic_qualification_pin import _sha
from photo_worker.v2.semantic_qualification_loader import _qualification_loader
from photo_worker.v2.semantic_qualification_report import verify_exposure_qualification_report
from .exposure_color import local_exposure_icc_verifier
from .preview_adapter import ExposurePreviewInstallation, PreviewPresentationPins, preview_inputs_declared


def sql_exposure_preview_factory(*,database,objects,bucket,icc_registry,
        model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha,presentation=None):
    if presentation is not None and not isinstance(presentation,PreviewPresentationPins):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    expected=(model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha)
    if (any(not _sha(value) for value in expected)
            or any(not callable(getattr(database,name,None)) for name in
                ('admit_semantic_exposure_preview','exposure_preview_protection_origin','exposure_preview_report_evidence'))
            or not callable(getattr(objects,'get_object',None))
            or not isinstance(bucket,str) or not 3<=len(bucket)<=63):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    verify_color=local_exposure_icc_verifier(icc_registry)
    def install(ctx,artifacts):
        lease=deepcopy(ctx.lease);profile=deepcopy(lease.execution_profile)
        def active():
            ctx.check_active()
            if ctx.lease!=lease:
                raise AdoptionInvalid('SEMANTIC_LEASE_CHANGED')
        active()
        command=lease.command
        if (command.operation!='semantic.preview_local_exposure' or command.operation_version!='1'
                or command.resource_class!='cpu' or profile.get('backend')!='cpu'
                or profile.get('renderer_version')!='v2.2'):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_TASK_INVALID')
        pin=ExposureQualificationPin.from_value(profile.get('qualification'))
        if (pin.model_lock_sha,pin.runtime_lock_sha,pin.device_profile_sha,pin.dataset_sha)!=expected:
            raise AdoptionInvalid('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')
        raw=artifacts.contract_bytes(command.params,SemanticExposurePreviewInput)
        params=SemanticExposurePreviewInput.model_validate_json(raw)
        from hashlib import sha256
        if canonical_bytes(params)!=raw or sha256(raw).hexdigest()!=command.params.sha256:
            raise AdoptionInvalid('SEMANTIC_PREVIEW_BYTES_INVALID')
        if ((params.model_lock_sha, params.runtime_lock_sha, params.device_profile_sha)
                != (pin.model_lock_sha, pin.runtime_lock_sha, pin.device_profile_sha)):
            raise AdoptionInvalid('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')
        if (params.tenant_id != command.tenant_id
                or not preview_inputs_declared(command, params)):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_SCOPE_INVALID')
        def evidence(owner):
            active()
            if owner!=lease:
                raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
            value=database.exposure_preview_report_evidence(deepcopy(lease),raw)
            active();return value
        report=_qualification_loader(SimpleNamespace(semantic_report_evidence=evidence),
            lease,objects,bucket,frozen_pin=pin,pin_type=ExposureQualificationPin,
            verify_report=verify_exposure_qualification_report,check_active=active)
        def runtime(value,supplied_profile):
            active()
            if canonical_bytes(value)!=raw or canonical_bytes(supplied_profile)!=canonical_bytes(profile):
                return False
            report();active();return True
        def admit(owner,body):
            active()
            if owner!=lease or body!=raw:return False
            accepted=database.admit_semantic_exposure_preview(deepcopy(lease),raw) is True
            active();return accepted
        def origin(owner,body,binding,root,binary):
            active()
            if owner!=lease or body!=raw or binding!=params.source or root!=params.protection:
                return False
            accepted=database.exposure_preview_protection_origin(deepcopy(lease),raw,root,binary) is True
            active();return accepted
        installation=ExposurePreviewInstallation(admit,origin,verify_color,runtime,presentation)
        if not runtime(params,profile) or not admit(lease,raw):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        return installation
    return install
