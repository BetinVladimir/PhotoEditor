from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic.preview_adapter import ExposurePreviewInstallation,execute_preview_task,exposure_preview_operations
from photo_worker.runtime.executor import OperationFailure


def test_preview_adapter_requires_explicit_installer():
    with pytest.raises(ValueError):exposure_preview_operations(None,installation_factory=None)
    with pytest.raises(ValueError):ExposurePreviewInstallation(None,lambda *a:True,lambda *a:True,lambda *a:True)


@pytest.mark.parametrize('failure',['apply','backend','vram','budget'])
def test_preview_adapter_rejects_wrong_task_before_artifact_reads(failure):
    command=SimpleNamespace(operation='semantic.preview_local_exposure',operation_version='1',resource_class='cpu')
    profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=dict(ram_bytes=128*1024**2,scratch_bytes=2*1024**2,vram_bytes=0,output_bytes=2*1024**2))
    if failure=='apply':command.operation='semantic.local_exposure'
    if failure=='backend':profile['backend']='mps'
    if failure=='vram':profile['resource_estimate']['vram_bytes']=1
    if failure=='budget':profile['resource_estimate']['ram_bytes']=True
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,execution_profile=profile),check_active=lambda:None)
    installation=ExposurePreviewInstallation(*(lambda *args:True for _ in range(4)))
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('invalid task read artifacts')
    with pytest.raises(OperationFailure):execute_preview_task(ctx,Forbidden(),installation)


@pytest.mark.parametrize('failure',['tenant','params_hash','qualification'])
def test_preview_adapter_requires_exact_task_scope_before_source_io(failure):
    from uuid import uuid4,UUID
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from test_v27_exposure_preview_binding import fixture
    params,_,_=fixture();raw=canonical_bytes(params)
    from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
    value=SemanticExposurePreviewInput.model_validate_json(raw)
    declared=[value.plan,value.intent,value.candidate,value.source.source,value.protection]
    command=SimpleNamespace(operation='semantic.preview_local_exposure',operation_version='1',resource_class='cpu',
        tenant_id=uuid4() if failure=='tenant' else UUID(params['tenant_id']),
        inputs=declared,params=SimpleNamespace(sha256='0'*64 if failure=='params_hash' else sha256(raw).hexdigest()))
    profile=dict(backend='cpu',renderer_version='v2.2',qualification=None,
        resource_estimate=dict(ram_bytes=128*1024**2,scratch_bytes=2*1024**2,vram_bytes=0,output_bytes=2*1024**2))
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command,execution_profile=profile),check_active=lambda:None)
    class Artifacts:
        calls=0
        def contract_bytes(self,*args):
            self.calls+=1
            if self.calls>1:raise AssertionError('source read before scope validation')
            return raw
    artifacts=Artifacts();installation=ExposurePreviewInstallation(*(lambda *args:True for _ in range(4)))
    with pytest.raises(OperationFailure):execute_preview_task(ctx,artifacts,installation)
    assert artifacts.calls==1


@pytest.mark.parametrize('failure',[None,'missing','duplicate','sha','zero'])
def test_preview_task_requires_exact_declared_proposal_roots(failure):
    from uuid import UUID
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
    from photo_worker.operations.semantic.preview_adapter import preview_inputs_declared
    from test_v27_exposure_preview_binding import fixture
    params,_,_=fixture();value=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    refs=[ref.model_copy(deep=True) for ref in (value.plan,value.intent,value.candidate,value.source.source,value.protection)]
    if failure=='missing':refs.pop()
    if failure=='duplicate':refs.append(refs[0])
    if failure=='sha':refs[0].sha256='0'*64
    if failure=='zero':refs[0].artifact_id=UUID(int=0)
    assert preview_inputs_declared(SimpleNamespace(inputs=refs),value) is (failure is None)


@pytest.mark.parametrize('root,lock,srgb', [
    ('relative', 'a'*64, 'b'*64),
    ('/profiles', 'invalid', 'b'*64),
    ('/profiles', 'a'*64, 'invalid'),
])
def test_presentation_installation_rejects_unpinned_color_context(root,lock,srgb):
    from pathlib import Path
    from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_INSTALLATION_REQUIRED'):
        PreviewPresentationPins(Path(root),lock,srgb)


def test_presentation_installation_requires_typed_pins():
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_INSTALLATION_REQUIRED'):
        ExposurePreviewInstallation(*(lambda *args:True for _ in range(4)),presentation={})
