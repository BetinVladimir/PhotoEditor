"""Adopter creates the operator builder with its actual object store."""

from hashlib import sha256
from threading import Event
from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2 import service
from photo_worker.v2.interchange_import_adopter_installation import (
    SchedulerImportRecipeAdoptionInstallation)
from photo_worker.v2 import interchange_import_adopter_schema as schema_module
from photo_worker.v2 import interchange_import_adopter_runtime as runtime_module


@pytest.mark.parametrize('stop_after', (1, 2))
def test_adopter_operator_factory_uses_live_store_and_scheduler_connection(tmp_path, monkeypatch, stop_after):
    objects = SimpleNamespace(get_object=lambda *_args, **_kwargs: None)
    monkeypatch.setattr(service, 'Minio', lambda *_args, **_kwargs: objects)
    connections = []
    class Connection:
        autocommit = True
        def __enter__(self):
            connections.append(self)
            return self
        def __exit__(self, *_args):
            return False
    monkeypatch.setattr(service.psycopg, 'connect', lambda *_args, **_kwargs: Connection())
    monkeypatch.setattr(schema_module, 'require_interchange_import_adopter_schema',
                        lambda connection: None)
    monkeypatch.setattr(runtime_module, 'require_import_adopter_runtime_schema',
                        lambda connection: None)
    lease_connections = []
    sweeps = []
    class Lease:
        def __init__(self, connection, signal):
            assert signal is stop
            lease_connections.append(connection)
        def __enter__(self):
            assert len(sweeps) == 1
            return self
        def __exit__(self, *_args):
            return False
    monkeypatch.setattr(runtime_module, 'ImportAdopterRuntimeLease', Lease)
    stop = Event()
    captured = []
    class Adopter:
        def __init__(self, read, write, store, bucket, **options):
            captured.append((read, write, store, bucket, options))
        def run_once(self):
            sweeps.append(len(sweeps) + 1)
            if len(sweeps) == stop_after:
                stop.set()
    monkeypatch.setattr(service, 'Adopter', Adopter)
    schemas = canonical_bytes({'$defs': {}})
    def operator_factory(store, bucket):
        assert store is objects and bucket == 'photo'
        def builder(connection, signal):
            assert signal is stop
            return SchedulerImportRecipeAdoptionInstallation(
                connection, schemas_jcs=schemas,
                schemas_sha=sha256(schemas).hexdigest(), work_icc_sha='a' * 64,
                scratch_root=tmp_path, scratch_bytes=1 << 20,
                max_file_bytes=1 << 20, qualify_runtime=lambda *_args: False,
                check_active=lambda: None)
        return builder
    config = service.AdopterConfig('postgres://unused', 'https://example.test',
                                   'photo', 'key', 'secret')
    service.run_adopter(config, stop, interchange_recipe_operator_factory=operator_factory)
    assert len(captured) == 1 and len(connections) == 3
    assert captured[0][:4] == (connections[0], connections[1], objects, 'photo')
    assert captured[0][4]['interchange_recipe_qualifier_factory'].connection is connections[0]
    assert sweeps == list(range(1, stop_after + 1))
    assert lease_connections == ([connections[2]] if stop_after == 2 else [])


def test_adopter_operator_factory_rejects_conflict_before_store_creation():
    config = service.AdopterConfig('postgres://unused', 'https://example.test',
                                   'photo', 'key', 'secret')
    with pytest.raises(ValueError, match='Conflicting'):
        service.run_adopter(config, Event(),
            interchange_recipe_operator_factory=lambda *_args: None,
            interchange_recipe_qualifier_builder=lambda *_args: None)
