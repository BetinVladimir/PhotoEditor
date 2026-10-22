"""Authored installation tests; execution belongs to the deferred test phase."""
import pytest
from photo_worker.operations.interchange.installation import (
    interchange_enabled_from_env, resolve_interchange_installation,
)
from photo_worker.operations.interchange.parse import ParseInstallation
from photo_worker.operations.interchange.admission import SQLParseAdmission


class Database:
    def interchange_parse_context(self, lease, params):
        raise AssertionError("installation must not execute SQL")


def test_interchange_enablement_is_explicit_and_cpu_only():
    assert interchange_enabled_from_env({}, "cpu") is False
    assert interchange_enabled_from_env({"PHOTO_INTERCHANGE_ENABLED": "true"}, "cpu") is True
    for value in ("1", "yes", "TRUE", "", " true"):
        with pytest.raises(ValueError):
            interchange_enabled_from_env({"PHOTO_INTERCHANGE_ENABLED": value}, "cpu")
    with pytest.raises(ValueError):
        interchange_enabled_from_env({"PHOTO_INTERCHANGE_ENABLED": "true"}, "gpu")


def test_interchange_sql_authority_and_custom_installation_are_exclusive():
    database = Database()
    installed = resolve_interchange_installation(database, True)
    assert isinstance(installed, ParseInstallation)
    assert isinstance(installed.admission, SQLParseAdmission)
    assert installed.admission.database is database
    assert resolve_interchange_installation(database, False) is None
    assert resolve_interchange_installation(database, False, installed) is installed
    with pytest.raises(ValueError):
        resolve_interchange_installation(database, True, installed)
    with pytest.raises(ValueError):
        resolve_interchange_installation(database, 1)
    with pytest.raises(ValueError):
        resolve_interchange_installation(object(), True)
    with pytest.raises(ValueError):
        resolve_interchange_installation(database, False, object())


def test_import_installation_is_explicit_and_validates_ports_without_io():
    from photo_worker.operations.interchange.installation import (
        resolve_interchange_import_installation, sql_import_installation,
    )
    from photo_worker.operations.interchange.import_adapter import ImportInstallation
    from photo_worker.operations.interchange.import_admission import SQLImportAdmission

    class ImportDatabase:
        def interchange_import_context(self, lease, raw):
            raise AssertionError("construction must not execute SQL")

    factory = lambda ctx, artifacts, authority: None
    installed = sql_import_installation(ImportDatabase(), factory)
    assert isinstance(installed.admission, SQLImportAdmission)
    assert installed.recipe_qualifier_factory is factory
    assert resolve_interchange_import_installation() is None
    assert resolve_interchange_import_installation(installed) is installed
    for invalid in (object(), ParseInstallation(object()), ImportInstallation(object()),
                    ImportInstallation(installed.admission, object())):
        with pytest.raises(ValueError):
            resolve_interchange_import_installation(invalid)
    with pytest.raises(ValueError):
        sql_import_installation(ImportDatabase(), None)
    with pytest.raises(ValueError):
        sql_import_installation(object(), factory)


@pytest.mark.parametrize("resource_class,backend", [("gpu", "mps"), ("cpu", "mps"), ("gpu", "cuda")])
def test_export_runtime_rejects_non_cpu_before_constructing_services(resource_class, backend):
    from types import SimpleNamespace
    from photo_worker.runtime.service import build_worker, WorkerConfigurationError
    config = SimpleNamespace(interchange_enabled=False, resource_class=resource_class, backend=backend)
    with pytest.raises(WorkerConfigurationError, match="Interchange runtime requires CPU worker"):
        build_worker(config, interchange_export_installation=object())


def test_run_worker_forwards_explicit_export_installation(monkeypatch):
    from photo_worker.runtime import service
    installed = object()
    class ConstructionStopped(Exception):
        pass
    def build(config, **kwargs):
        assert kwargs["interchange_export_installation"] is installed
        raise ConstructionStopped()
    monkeypatch.setattr(service, "build_worker", build)
    with pytest.raises(ConstructionStopped):
        service.run_worker(__import__("types").SimpleNamespace(capacity_policy=None, backend="cpu", resource_class="cpu"), interchange_export_installation=installed)


def test_invalid_export_installation_is_rejected_before_tls_or_database():
    from types import SimpleNamespace
    from photo_worker.runtime.service import build_worker, WorkerConfigurationError
    config = SimpleNamespace(interchange_enabled=False, resource_class="cpu", backend="cpu")
    with pytest.raises(WorkerConfigurationError, match="INTERCHANGE_EXPORT_INSTALLATION_INVALID"):
        build_worker(config, interchange_export_installation=object())


@pytest.mark.parametrize("enabled", [False, True])
def test_export_runtime_registers_only_explicit_installation(monkeypatch, tmp_path, enabled):
    from types import SimpleNamespace
    from uuid import uuid4
    from photo_worker.runtime import service
    from photo_worker.runtime.executor import Operation
    from photo_worker.operations.layers import adapter as layers
    from photo_worker.operations.interchange import export_adapter
    config = service.WorkerConfig("unused-db", "unused-amqp", uuid4(), "cpu", tmp_path,
        "https://unused.invalid", tmp_path/"ca", tmp_path/"cert", tmp_path/"key", 1024, 1024, 1024)
    # Isolate unrelated operation constructors, while retaining real Executor and
    # interchange_export_operation registration and callback wiring.
    for name, value in tuple(vars(service).items()):
        if name.endswith("_operations") and callable(value):
            monkeypatch.setattr(service, name, lambda *args, **kwargs: [])
    monkeypatch.setattr(service, "asset_operations", lambda *args: [Operation("test.base", "cpu", lambda ctx: None)])
    monkeypatch.setattr(layers, "layer_operations", lambda *args: [])
    monkeypatch.setattr(service, "client_tls", lambda *args: object())
    database = SimpleNamespace(media_renderer_policy_ready=lambda:True, task_result_fence_ready=lambda:True, editor_internal_dispatch_installed=lambda operation:True)
    monkeypatch.setattr(service, "TaskDatabase", lambda *args: database)
    monkeypatch.setattr(service, "Transport", lambda url, resource, executor, **kwargs: SimpleNamespace(executor=executor))
    installed = export_adapter.ExportInstallation(SimpleNamespace(admit=lambda *args: None), lambda *args: None)
    returned, transport = service.build_worker(config, interchange_export_installation=installed if enabled else None)
    assert returned is database
    assert ("v2.interchange.export" in transport.executor.operations) is enabled
    if not enabled:
        return
    operation = transport.executor.operations["v2.interchange.export"]
    assert operation.resource_class == "cpu"
    from contextlib import contextmanager
    artifacts, context, expected = object(), object(), object()
    @contextmanager
    def create(ctx, artifact_config, roles):
        assert ctx is context
        assert roles == frozenset({"bundle", "report", "mapping_shard", "document"})
        yield artifacts
    def execute(ctx, received, installation):
        assert ctx is context and received is artifacts and installation is installed
        return expected
    monkeypatch.setattr(export_adapter.TaskArtifacts, "create", create)
    monkeypatch.setattr(export_adapter, "execute_export", execute)
    assert operation.execute(context) is expected


@pytest.mark.parametrize('sql_ready', [False, True])
def test_import_worker_requires_current_person_mask_sql_before_registration(monkeypatch, tmp_path, sql_ready):
    from types import SimpleNamespace
    from uuid import uuid4
    from photo_worker.runtime import service
    from photo_worker.runtime.executor import Operation
    from photo_worker.operations.layers import adapter as layers
    from photo_worker.operations.interchange.import_adapter import ImportInstallation
    config = service.WorkerConfig('unused-db', 'unused-amqp', uuid4(), 'cpu', tmp_path,
        'https://unused.invalid', tmp_path/'ca', tmp_path/'cert', tmp_path/'key', 1024, 1024, 1024)
    for name, value in tuple(vars(service).items()):
        if name.endswith('_operations') and callable(value):
            monkeypatch.setattr(service, name, lambda *args, **kwargs: [])
    monkeypatch.setattr(service, 'asset_operations', lambda *args: [Operation('test.base', 'cpu', lambda ctx: None)])
    monkeypatch.setattr(layers, 'layer_operations', lambda *args: [])
    monkeypatch.setattr(service, 'client_tls', lambda *args: object())
    database = SimpleNamespace(interchange_import_person_mask_ready=lambda: sql_ready, media_renderer_policy_ready=lambda:True, task_result_fence_ready=lambda:True, editor_internal_dispatch_installed=lambda operation:True)
    monkeypatch.setattr(service, 'TaskDatabase', lambda *args: database)
    monkeypatch.setattr(service, 'Transport', lambda url, resource, executor, **kwargs: SimpleNamespace(executor=executor))
    installed = ImportInstallation(SimpleNamespace(admit=lambda *args: None), lambda *args: None)
    if not sql_ready:
        with pytest.raises(service.WorkerConfigurationError, match='person-mask SQL authority'):
            service.build_worker(config, interchange_import_installation=installed)
    else:
        returned, transport = service.build_worker(config, interchange_import_installation=installed)
        assert returned is database
        assert 'v2.interchange.import' in transport.executor.operations
