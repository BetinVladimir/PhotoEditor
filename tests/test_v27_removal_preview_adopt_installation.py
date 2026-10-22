"""Scope failures must precede qualification SQL/storage IO."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import ShownRemovalAdoptionInput
from photo_worker.operations.semantic import preview_removal_adopt_installation as module
from photo_worker.v2.adoption import AdoptionInvalid
from test_v27_removal_preview_adopt_execute import fixture


@pytest.mark.parametrize('failure',['tenant','child','missing','duplicate','sha','model'])
def test_copy_installer_rejects_scope_before_external_io(monkeypatch,failure):
    params=ShownRemovalAdoptionInput.model_validate_json(canonical_bytes(fixture()));raw=canonical_bytes(params)
    refs=[ref.model_copy(deep=True) for ref in (params.approved_edit,params.presentation,
        params.numerical_evidence,params.working_image,params.pixels)]
    if failure=='missing':refs.pop()
    if failure=='duplicate':refs.append(refs[0])
    if failure=='sha':refs[0].sha256='f'*64
    expected=(params.model_lock_sha,params.runtime_lock_sha,params.device_profile_sha,'d'*64)
    pin=SimpleNamespace(**dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    if failure=='model':expected=('f'*64,*expected[1:])
    command=SimpleNamespace(operation='semantic.adopt_shown_removal',operation_version='1',resource_class='cpu',
        tenant_id=uuid4() if failure=='tenant' else params.tenant_id,
        run_id=uuid4() if failure=='child' else params.child_run_id,inputs=refs,
        params=SimpleNamespace(sha256=sha256(raw).hexdigest()))
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'backend':'cpu','renderer_version':'v2.2','qualification':{}}),check_active=lambda:None)
    monkeypatch.setattr(module,'ShownRemovalQualificationPin',SimpleNamespace(from_value=lambda value:pin))
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry:lambda color:True)
    def forbidden(*args,**kwargs):raise AssertionError('invalid scope reached external IO')
    monkeypatch.setattr(module,'_qualification_loader',forbidden)
    factory=module.sql_shown_removal_factory(database=SimpleNamespace(admit_semantic_shown_removal=forbidden,
        shown_removal_report_evidence=forbidden),objects=SimpleNamespace(get_object=forbidden),bucket='photos',icc_registry=object(),
        **dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    with pytest.raises(AdoptionInvalid):factory(ctx,SimpleNamespace(contract_bytes=lambda *args:raw,declared_input_refs=lambda **kwargs:tuple(refs)))
