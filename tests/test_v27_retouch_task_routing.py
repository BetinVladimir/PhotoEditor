"""Wrong backend/operation/renderer must fail before artifact or installer IO."""
from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic import preview_retouch_adapter as preview
from photo_worker.operations.semantic import preview_retouch_adopt_adapter as shown
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.operations.semantic.task_route import require_cpu_semantic_route
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('kind', ['preview','shown'])
@pytest.mark.parametrize('mutation', [
    'operation','version','resource','cuda','mps','missing_backend',
    'renderer','missing_renderer','non_object_profile','missing_lease',
])
def test_registered_retouch_rejects_wrong_route_before_any_io(monkeypatch, tmp_path, kind, mutation):
    name = 'semantic.preview_retouch_region' if kind == 'preview' else 'semantic.adopt_shown_retouch_region'
    command = SimpleNamespace(operation=name,operation_version='1',resource_class='cpu')
    profile = dict(backend='cpu',renderer_version='v2.2')
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command,execution_profile=profile))
    if mutation == 'operation':command.operation='semantic.retouch'
    elif mutation == 'version':command.operation_version='2'
    elif mutation == 'resource':command.resource_class='gpu'
    elif mutation in ('cuda','mps'):profile['backend']=mutation
    elif mutation == 'missing_backend':del profile['backend']
    elif mutation == 'renderer':profile['renderer_version']='v2.1'
    elif mutation == 'missing_renderer':del profile['renderer_version']
    elif mutation == 'non_object_profile':ctx.lease.execution_profile=None
    elif mutation == 'missing_lease':del ctx.lease
    def forbidden(*args,**kwargs):raise AssertionError('wrong route performed IO')
    module = preview if kind == 'preview' else shown
    monkeypatch.setattr(module,'TaskArtifacts',SimpleNamespace(create=forbidden))
    if kind == 'preview':
        operation = preview.retouch_preview_operations(None,installation_factory=forbidden,
            presentation=PreviewPresentationPins(tmp_path,'a'*64,'b'*64))[0]
    else:operation = shown.shown_retouch_operations(None,installation_factory=forbidden)[0]
    with pytest.raises(OperationFailure) as caught:operation.execute(ctx)
    assert caught.value.error.code == 'CONFIG_INVALID'


@pytest.mark.parametrize('name', ['semantic.preview_retouch_region','semantic.adopt_shown_retouch_region'])
def test_cpu_route_requires_no_gpu_backend_or_vram(name):
    ctx = SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation=name,operation_version='1',resource_class='cpu'),
        execution_profile=dict(backend='cpu',renderer_version='v2.2')))
    require_cpu_semantic_route(ctx,name)
