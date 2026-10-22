from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef
import pytest
from types import SimpleNamespace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic.preview_adapter import ExposurePreviewInstallation
from photo_worker.operations.semantic.preview_removal_adapter import (
    removal_preview_operations, execute_removal_preview_task,
)


def test_exposure_installer_cannot_admit_removal():
    installation=ExposurePreviewInstallation(lambda *a:True,lambda *a:True,
        lambda *a:True,lambda *a:True)
    with pytest.raises(OperationFailure):
        execute_removal_preview_task(None,None,installation,None,'cuda')


@pytest.mark.parametrize('backend', ['cuda','mps','cpu'])
def test_explicit_backend_registration_requires_installer_and_pool(backend):
    with pytest.raises(ValueError):
        removal_preview_operations(None,installation_factory=None,pool=None,backend=backend)
    with pytest.raises(ValueError):
        removal_preview_operations(None,installation_factory=lambda *a:None,pool=None,backend=backend)
    operations=removal_preview_operations(None,installation_factory=lambda *a:None,
        pool=SimpleNamespace(acquire=lambda *a:None),backend=backend)
    assert len(operations)==1


@pytest.mark.parametrize('backend,device', [('cpu',1),('cuda',0),
    ('cuda',10_000_000_001),('mps',0)])
def test_removal_invalid_device_budget_refuses_before_model_and_artifact_io(backend,device):
    from photo_worker.operations.semantic.preview_removal_adapter import RemovalPreviewInstallation
    def forbidden(*args,**kwargs):
        raise AssertionError('invalid resource profile reached execution')
    installation=RemovalPreviewInstallation(*(forbidden for _ in range(4)))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        command=SimpleNamespace(operation='semantic.preview_remove_object',operation_version='1',resource_class='gpu'),
        execution_profile=dict(backend=backend,renderer_version='v2.2',resource_estimate=dict(
            ram_bytes=1,scratch_bytes=1,vram_bytes=device,output_bytes=1))))
    artifacts=SimpleNamespace(contract_bytes=forbidden)
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        execute_removal_preview_task(ctx,artifacts,installation,SimpleNamespace(acquire=forbidden),backend)


@pytest.mark.parametrize('backend,device', [('cpu',0),('cuda',10_000_000_000),('mps',12_000_000_000)])
def test_removal_valid_device_policy_reaches_exact_contract_boundary(backend,device):
    from photo_worker.operations.semantic.preview_removal_adapter import RemovalPreviewInstallation
    class ContractBoundaryReached(Exception):
        pass
    def forbidden(*args,**kwargs):
        raise AssertionError('model must not run in resource boundary scenario')
    installation=RemovalPreviewInstallation(*(forbidden for _ in range(4)))
    command=SimpleNamespace(operation='semantic.preview_remove_object',operation_version='1',resource_class='gpu',params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command,
        execution_profile=dict(backend=backend,renderer_version='v2.2',resource_estimate=dict(
            ram_bytes=1,scratch_bytes=1,vram_bytes=device,output_bytes=1))))
    def read_contract(*args):
        raise ContractBoundaryReached()
    with pytest.raises(ContractBoundaryReached):
        execute_removal_preview_task(ctx,SimpleNamespace(contract_bytes=read_contract),installation,None,backend)
