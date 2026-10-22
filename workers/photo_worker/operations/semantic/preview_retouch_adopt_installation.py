"""Explicit live SQL and exact-version qualification installer for byte copy."""
from copy import deepcopy
from .preview_retouch_inputs import retouch_declared_inputs
from hashlib import sha256
from types import SimpleNamespace
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_qualification_pin import _sha
from photo_worker.v2.semantic_qualification_loader import _qualification_loader
from photo_worker.v2.shown_retouch_qualification import (
    ShownRetouchQualificationPin,verify_shown_retouch_qualification_report)
from .exposure_color import local_exposure_icc_verifier
from .preview_retouch_adopt_adapter import ShownRetouchInstallation


def sql_shown_retouch_factory(*,database,objects,bucket,icc_registry,
        model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha):
    expected=(model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha)
    if (any(not _sha(value) for value in expected)
            or any(not callable(getattr(database,name,None)) for name in
                ('admit_semantic_shown_retouch','shown_retouch_report_evidence'))
            or not callable(getattr(objects,'get_object',None))
            or not isinstance(bucket,str) or not 3<=len(bucket)<=63):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    verify_color=local_exposure_icc_verifier(icc_registry)
    def install(ctx,artifacts):
        lease=deepcopy(ctx.lease);profile=deepcopy(lease.execution_profile)
        def active():
            ctx.check_active()
            if ctx.lease!=lease:raise AdoptionInvalid('SEMANTIC_LEASE_CHANGED')
        active();command=lease.command
        if (command.operation!='semantic.adopt_shown_retouch_region' or command.operation_version!='1'
                or command.resource_class!='cpu' or profile.get('backend')!='cpu'
                or profile.get('renderer_version')!='v2.2'):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_TASK_INVALID')
        pin=ShownRetouchQualificationPin.from_value(profile.get('qualification'))
        if (pin.model_lock_sha,pin.runtime_lock_sha,pin.device_profile_sha,pin.dataset_sha)!=expected:
            raise AdoptionInvalid('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')
        raw=artifacts.contract_bytes(command.params,ShownRetouchAdoptionInput)
        active()
        params=ShownRetouchAdoptionInput.model_validate_json(raw)
        if canonical_bytes(params)!=raw or sha256(raw).hexdigest()!=command.params.sha256:
            raise AdoptionInvalid('SEMANTIC_PREVIEW_BYTES_INVALID')
        if (params.model_lock_sha,params.runtime_lock_sha,params.device_profile_sha)!=expected[:3]:
            raise AdoptionInvalid('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')
        try:
            inputs=list(retouch_declared_inputs(artifacts,check_active=active))
        except ValueError:
            raise AdoptionInvalid('SEMANTIC_PREVIEW_SCOPE_INVALID') from None
        if (params.tenant_id!=command.tenant_id or params.child_run_id!=command.run_id
                or not isinstance(inputs,list) or not 1<=len(inputs)<=512
                or any(not isinstance(ref,ArtifactRef) or ref.artifact_id.int==0 for ref in inputs)):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_SCOPE_INVALID')
        declared={ref.artifact_id:ref.sha256 for ref in inputs}
        if len(declared)!=len(inputs) or any(declared.get(ref.artifact_id)!=ref.sha256 for ref in
                (params.approved_edit,params.presentation,params.numerical_evidence,params.working_image,params.pixels)):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_SCOPE_INVALID')
        def evidence(owner):
            active()
            if owner!=lease:raise AdoptionInvalid('SEMANTIC_QUALIFICATION_SCOPE_STALE')
            value=database.shown_retouch_report_evidence(deepcopy(lease),raw)
            active();return value
        report=_qualification_loader(SimpleNamespace(semantic_report_evidence=evidence),lease,objects,bucket,
            frozen_pin=pin,pin_type=ShownRetouchQualificationPin,
            verify_report=verify_shown_retouch_qualification_report,check_active=active)
        def runtime(value,supplied_profile):
            active()
            if canonical_bytes(value)!=raw or canonical_bytes(supplied_profile)!=canonical_bytes(profile):return False
            report();active();return True
        def admit(owner,body):
            active()
            if owner!=lease or body!=raw:return False
            accepted=database.admit_semantic_shown_retouch(deepcopy(lease),raw) is True
            active();return accepted
        if not runtime(params,profile) or not admit(lease,raw):
            raise AdoptionInvalid('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        return ShownRetouchInstallation(admit,runtime,verify_color)
    return install
