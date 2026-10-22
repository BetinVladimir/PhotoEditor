"""Scope failures must precede qualification SQL/storage IO."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
from photo_worker.operations.semantic import preview_retouch_adopt_installation as module
from photo_worker.v2.adoption import AdoptionInvalid
from test_v27_shown_retouch_task_fixture import fixture


@pytest.mark.parametrize('failure',['tenant','child','missing','duplicate','sha','model','index_missing','index_sha','index_duplicate','index_absent'])
def test_copy_installer_rejects_scope_before_external_io(monkeypatch,failure):
    params=ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(fixture()));raw=canonical_bytes(params)
    refs=[ref.model_copy(deep=True) for ref in (params.approved_edit,params.presentation,
        params.numerical_evidence,params.working_image,params.pixels)]
    if failure=='missing':refs.pop()
    if failure=='duplicate':refs.append(refs[0])
    if failure=='sha':refs[0].sha256='f'*64
    expected=(params.model_lock_sha,params.runtime_lock_sha,params.device_profile_sha,'d'*64)
    pin=SimpleNamespace(**dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    if failure=='model':expected=('f'*64,*expected[1:])
    command=SimpleNamespace(operation='semantic.adopt_shown_retouch_region',operation_version='1',resource_class='cpu',
        tenant_id=uuid4() if failure=='tenant' else params.tenant_id,
        run_id=uuid4() if failure=='child' else params.child_run_id,inputs=refs,
        params=SimpleNamespace(sha256=sha256(raw).hexdigest()))
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'backend':'cpu','renderer_version':'v2.2','qualification':{}}),check_active=lambda:None)
    monkeypatch.setattr(module,'ShownRetouchQualificationPin',SimpleNamespace(from_value=lambda value:pin))
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry:lambda color:True)
    def forbidden(*args,**kwargs):raise AssertionError('invalid scope reached external IO')
    monkeypatch.setattr(module,'_qualification_loader',forbidden)
    factory=module.sql_shown_retouch_factory(database=SimpleNamespace(admit_semantic_shown_retouch=forbidden,
        shown_retouch_report_evidence=forbidden),objects=SimpleNamespace(get_object=forbidden),bucket='photos',icc_registry=object(),
        **dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    index_refs=[ref.model_copy(deep=True) for ref in refs]
    if failure=='index_missing':index_refs.pop()
    if failure=='index_sha':index_refs[0].sha256='e'*64
    if failure=='index_duplicate':index_refs.append(index_refs[0])
    artifacts=SimpleNamespace(contract_bytes=lambda *args:raw)
    if failure!='index_absent':artifacts.declared_input_refs=lambda **kwargs:tuple(index_refs)
    with pytest.raises(AdoptionInvalid):factory(ctx,artifacts)


@pytest.mark.parametrize('loss',['cancel','lease'])
def test_copy_installer_fences_params_read_before_parsing(monkeypatch,loss):
    expected=('a'*64,'b'*64,'c'*64,'d'*64)
    pin=SimpleNamespace(**dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    cancelled=False
    command=SimpleNamespace(operation='semantic.adopt_shown_retouch_region',operation_version='1',
        resource_class='cpu',params=None)
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,
        execution_profile={'backend':'cpu','renderer_version':'v2.2','qualification':{}},fencing_token=1))
    def active():
        if cancelled:raise InterruptedError('cancelled during installer params read')
    ctx.check_active=active
    monkeypatch.setattr(module,'ShownRetouchQualificationPin',SimpleNamespace(from_value=lambda value:pin))
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry:lambda color:True)
    def forbidden(*a,**k):raise AssertionError('external authority reached after scope loss')
    monkeypatch.setattr(module,'_qualification_loader',forbidden)
    factory=module.sql_shown_retouch_factory(database=SimpleNamespace(
        admit_semantic_shown_retouch=forbidden,shown_retouch_report_evidence=forbidden),
        objects=SimpleNamespace(get_object=forbidden),bucket='photos',icc_registry=object(),
        **dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'),expected)))
    class Artifacts:
        def contract_bytes(self,*args):
            nonlocal cancelled
            if loss=='cancel':cancelled=True
            else:ctx.lease.fencing_token+=1
            return b'{}'
    with pytest.raises(InterruptedError if loss=='cancel' else AdoptionInvalid):
        factory(ctx,Artifacts())
