"""Installer scope must fail before qualification storage or SQL is touched."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
from photo_worker.operations.semantic import preview_installation as module
from photo_worker.v2.adoption import AdoptionInvalid
from test_v27_exposure_preview_binding import fixture


@pytest.mark.parametrize('failure',['tenant','missing','duplicate','sha','model','runtime','device'])
def test_installer_rejects_scope_before_qualification_io(monkeypatch,failure):
    params,_,_=fixture()
    value=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    raw=canonical_bytes(value)
    refs=[ref.model_copy(deep=True) for ref in
          (value.plan,value.intent,value.candidate,value.source.source,value.protection)]
    if failure=='missing':refs.pop()
    if failure=='duplicate':refs.append(refs[0])
    if failure=='sha':refs[0].sha256='0'*64
    command=SimpleNamespace(operation='semantic.preview_local_exposure',operation_version='1',
        resource_class='cpu',tenant_id=uuid4() if failure=='tenant' else value.tenant_id,
        inputs=refs,params=SimpleNamespace(sha256=sha256(raw).hexdigest()))
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'backend':'cpu','renderer_version':'v2.2','qualification':{}}),
        check_active=lambda:None)
    expected=(value.model_lock_sha,value.runtime_lock_sha,value.device_profile_sha,'d'*64)
    if failure in ('model','runtime','device'):
        index={'model':0,'runtime':1,'device':2}[failure]
        expected=tuple('f'*64 if i==index and item!='f'*64 else ('e'*64 if i==index else item)
                       for i,item in enumerate(expected))
    pin=SimpleNamespace(**dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    monkeypatch.setattr(module,'ExposureQualificationPin',SimpleNamespace(from_value=lambda value:pin))
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry:lambda color:True)
    def forbidden(*args,**kwargs):raise AssertionError('scope failure reached external IO')
    monkeypatch.setattr(module,'_qualification_loader',forbidden)
    database=SimpleNamespace(admit_semantic_exposure_preview=forbidden,
        exposure_preview_protection_origin=forbidden,exposure_preview_report_evidence=forbidden)
    factory=module.sql_exposure_preview_factory(database=database,
        objects=SimpleNamespace(get_object=forbidden),bucket='photos',icc_registry=object(),
        **dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    with pytest.raises(AdoptionInvalid,match=('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED'
            if failure in ('model','runtime','device') else 'SEMANTIC_PREVIEW_SCOPE_INVALID')):
        factory(ctx,SimpleNamespace(contract_bytes=lambda *args:raw))
