"""Startup ABI checks only; does not start transports or execute worker tasks."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime.service import _require_removal_operation_registration, WorkerConfigurationError
from photo_worker.runtime.executor import Operation


@pytest.mark.parametrize('preview,shown', [(False, False), (True, False), (False, True), (True, True)])
def test_removal_registration_matches_enabled_installations(preview, shown):
    operations = []
    if preview:operations.append(Operation('semantic.preview_remove_object', 'gpu', lambda _: None, '1'))
    if shown:operations.append(Operation('semantic.adopt_shown_removal', 'cpu', lambda _: None, '1'))
    _require_removal_operation_registration(operations, preview_enabled=preview, shown_enabled=shown)


@pytest.mark.parametrize('name,resource', [('semantic.preview_remove_object', 'gpu'), ('semantic.adopt_shown_removal', 'cpu')])
@pytest.mark.parametrize('failure', ['missing', 'duplicate', 'disabled', 'version', 'resource', 'callback', 'untyped'])
def test_removal_registration_rejects_abi_divergence(name, resource, failure):
    item = Operation(name, resource, None if failure == 'callback' else lambda _: None,
        '2' if failure == 'version' else '1')
    if failure == 'resource':item = Operation(name, 'cpu' if resource == 'gpu' else 'gpu', lambda _: None, '1')
    if failure == 'untyped':item = SimpleNamespace(name=name, resource_class=resource, execute=lambda _: None, version='1')
    operations = [] if failure == 'missing' else [item, item] if failure == 'duplicate' else [item]
    enabled = failure != 'disabled'
    with pytest.raises(WorkerConfigurationError):
        _require_removal_operation_registration(operations,
            preview_enabled=enabled and resource == 'gpu', shown_enabled=enabled and resource == 'cpu')
