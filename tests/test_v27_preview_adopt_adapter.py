from types import SimpleNamespace
import pytest
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic.preview_adopt_adapter import (ShownExposureInstallation,
    execute_shown_exposure_task,shown_exposure_operations)


def test_copy_adapter_requires_explicit_installer():
    with pytest.raises(ValueError):shown_exposure_operations(None,installation_factory=None)


@pytest.mark.parametrize('operation,backend',[('semantic.local_exposure','cpu'),('semantic.adopt_shown_exposure','cuda')])
def test_copy_adapter_rejects_rerender_or_wrong_backend_before_io(operation,backend):
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        command=SimpleNamespace(operation=operation,operation_version='1',resource_class='cpu'),
        execution_profile=dict(backend=backend,renderer_version='v2.2')))
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('invalid task IO')
    with pytest.raises(OperationFailure):
        execute_shown_exposure_task(ctx,Forbidden(),ShownExposureInstallation(*(lambda *args:True for _ in range(3))))
