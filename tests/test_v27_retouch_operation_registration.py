"""Registration and current catalog parity, authored without worker startup IO."""
from dataclasses import replace
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import json

import pytest

from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.operations.semantic.preview_retouch_adapter import retouch_preview_operations
from photo_worker.operations.semantic.preview_retouch_adopt_adapter import shown_retouch_operations
from photo_worker.runtime.executor import Executor
from photo_worker.runtime.service import _require_retouch_operation_registration, WorkerConfigurationError


def operations(tmp_path):
    return retouch_preview_operations(None, installation_factory=lambda *args: None,
        presentation=PreviewPresentationPins(tmp_path, 'a'*64, 'b'*64)) + shown_retouch_operations(
            None, installation_factory=lambda *args: None)


@pytest.mark.parametrize('preview,shown', [(False,False),(True,False),(False,True),(True,True)])
def test_exact_enabled_registration_matches_catalog_and_executor(tmp_path, preview, shown):
    available = operations(tmp_path)
    installed = ([available[0]] if preview else []) + ([available[1]] if shown else [])
    _require_retouch_operation_registration(installed, preview_enabled=preview, shown_enabled=shown)
    executor = Executor(SimpleNamespace(), uuid4(), installed)
    catalog = json.loads((Path(__file__).resolve().parents[1]/'backend/internal/workflow/operations.json').read_text())
    for operation in installed:
        rows = [row for row in catalog['operations'] if row['name'] == operation.name]
        assert len(rows) == 1
        row = rows[0]
        assert (row['version'], row['resource_class'], row['backends']) == ('1','cpu',['cpu'])
        assert row['version'] == operation.version and row['resource_class'] == operation.resource_class
        assert executor.operations[operation.name] is operation
        if operation is available[0]:
            assert (row['params_type'],row['output_type']) == ('SemanticRetouchPreviewInput','RetouchPreviewPresentation')
        else:
            assert (row['params_type'],row['output_type']) == ('ShownRetouchAdoptionInput','WorkingImage')


@pytest.mark.parametrize('index', [0,1])
@pytest.mark.parametrize('failure', ['missing','duplicate','version','resource','executor','name','disabled'])
def test_invalid_registration_prevents_executor_and_capacity_publication(tmp_path, index, failure):
    installed = operations(tmp_path)
    if failure == 'missing':
        installed.pop(index)
    elif failure == 'duplicate':
        installed.append(installed[index])
    elif failure == 'version':
        installed[index] = replace(installed[index], version='2')
    elif failure == 'resource':
        installed[index] = replace(installed[index], resource_class='gpu')
    elif failure == 'executor':
        installed[index] = replace(installed[index], execute=None)
    elif failure == 'name':
        installed[index] = replace(installed[index], name='semantic.retouch')
    with pytest.raises(WorkerConfigurationError, match='registration does not match installation'):
        _require_retouch_operation_registration(installed,
            preview_enabled=not(failure == 'disabled' and index == 0),
            shown_enabled=not(failure == 'disabled' and index == 1))


@pytest.mark.parametrize('preview,shown', [(False,False),(True,False),(False,True),(True,True)])
def test_run_worker_forwards_exact_retouch_installations(monkeypatch, preview, shown):
    from photo_worker.runtime import service
    config = SimpleNamespace(capacity_policy=None, resource_class="cpu", backend="cpu")
    preview_startup, shown_factory = object(), lambda *args: None
    database, transport = object(), object()
    calls = []
    def build(supplied_config, **kwargs):
        assert supplied_config is config
        expected = {}
        if preview: expected['retouch_preview_startup'] = preview_startup
        if shown: expected['shown_retouch_installation_factory'] = shown_factory
        assert kwargs == expected
        calls.append('build')
        return database, transport
    def run(supplied_config, supplied_database, supplied_transport, **kwargs):
        assert supplied_config is config and supplied_database is database and supplied_transport is transport
        calls.append('run')
    monkeypatch.setattr(service, '_install_metal_budget', lambda supplied: None)
    monkeypatch.setattr(service, 'build_worker', build)
    monkeypatch.setattr(service, 'run_built_worker', run)
    service.run_worker(config, retouch_preview_startup=preview_startup if preview else None,
        shown_retouch_installation_factory=shown_factory if shown else None)
    assert calls == ['build','run']
