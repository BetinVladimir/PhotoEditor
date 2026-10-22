"""Own installer boundary, with explicit qualification/SQL doubles and no GPU."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
from photo_worker.operations.semantic import preview_removal_installation as module
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.runtime.executor import TaskCancelled
from photo_worker.v2.adoption import AdoptionInvalid
from test_v27_removal_preview_binding import removal_fixture


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
@pytest.mark.parametrize('failure',[None,'tenant','hash','noncanonical','missing','duplicate','index_sha','no_index','params_cancel','index_lease'])
def test_installer_binds_full_index_before_qualification(monkeypatch,tmp_path,backend,failure):
    value,_,_=removal_fixture();params=SemanticRemovalPreviewInput.model_validate_json(canonical_bytes(value));raw=canonical_bytes(params)
    expected=dict(model_lock_sha=params.model_lock_sha,runtime_lock_sha=params.runtime_lock_sha,
        device_profile_sha=params.device_profile_sha,dataset_sha='d'*64)
    root=ArtifactRef(artifact_id=uuid4(),sha256='f'*64 if failure=='hash' else sha256(raw).hexdigest())
    if failure=='noncanonical':raw+=b' ';root.sha256=sha256(raw).hexdigest()
    refs=[ref.model_copy(deep=True) for ref in (params.plan,params.intent,params.candidate,params.source.source,params.protection)]
    if failure=='missing':refs.pop()
    if failure=='duplicate':refs.append(refs[0])
    if failure=='index_sha':refs[0].sha256='e'*64
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(operation='semantic.preview_remove_object',
        operation_version='1',resource_class='gpu',tenant_id=uuid4() if failure=='tenant' else params.tenant_id,
        params=root,inputs=[root]),execution_profile=dict(backend=backend,renderer_version='v2.2',qualification={}),fence=1))
    cancelled=False;events=[]
    def active():
        if cancelled:raise TaskCancelled('cancelled during Params read')
    ctx.check_active=active
    def read(*args):
        nonlocal cancelled
        if failure=='params_cancel':cancelled=True
        return raw
    def discover(*,maximum):
        assert maximum==512
        if failure=='index_lease':ctx.lease.fence+=1
        return tuple(refs)
    artifacts=SimpleNamespace(contract_bytes=read)
    if failure!='no_index':artifacts.declared_input_refs=discover
    monkeypatch.setattr(module,'RemovalQualificationPin',SimpleNamespace(from_value=lambda value:SimpleNamespace(backend=backend,**expected)))
    monkeypatch.setattr(module,'local_exposure_icc_verifier',lambda registry:lambda *args:True)
    def report(*args,**kwargs):
        events.append('qualification');return lambda:None
    monkeypatch.setattr(module,'_qualification_loader',report)
    def admit(*args):events.append('admission');return True
    factory=module.sql_removal_preview_factory(database=SimpleNamespace(admit_semantic_removal_preview=admit,
        removal_preview_protection_origin=lambda *args:True,removal_preview_report_evidence=lambda *args:None),
        objects=SimpleNamespace(get_object=lambda *args:None),bucket='photos',icc_registry=object(),
        backend=backend,presentation=PreviewPresentationPins(tmp_path,'a'*64,'b'*64),**expected)
    if failure is None:
        installed=factory(ctx,artifacts)
        assert isinstance(installed,module.RemovalPreviewInstallation)
        assert events==['qualification','admission']
    else:
        with pytest.raises(TaskCancelled if failure=='params_cancel' else AdoptionInvalid):factory(ctx,artifacts)
        assert events==[]
