"""Process-owned V30 importer is built against the live scheduler connection."""

from hashlib import sha256
from threading import Event

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_import_adopter_builder import build_interchange_import_adopter
from photo_worker.v2.interchange_import_adopter_installation import SchedulerImportRecipeAdoptionInstallation


class Connection:
    autocommit = True


def test_import_adopter_builder_binds_live_connection_and_stop(tmp_path):
    read, stop = Connection(), Event()
    schemas = canonical_bytes({'$defs': {}})
    calls = []
    def builder(connection, signal):
        calls.append((connection, signal))
        return SchedulerImportRecipeAdoptionInstallation(
            connection, schemas_jcs=schemas,
            schemas_sha=sha256(schemas).hexdigest(), work_icc_sha='a' * 64,
            scratch_root=tmp_path, scratch_bytes=1 << 20,
            max_file_bytes=1 << 20, qualify_runtime=lambda *_args: False,
            check_active=lambda: None)
    installed = build_interchange_import_adopter(builder, read, stop)
    assert installed.connection is read
    assert calls == [(read, stop)]
    stop.set()
    with pytest.raises(InterruptedError, match='stopped'):
        installed.active()


def test_import_adopter_builder_rejects_foreign_connection_or_missing_installation(tmp_path):
    read = Connection()
    schemas = canonical_bytes({'$defs': {}})
    def foreign(*_args):
        return SchedulerImportRecipeAdoptionInstallation(
            Connection(), schemas_jcs=schemas,
            schemas_sha=sha256(schemas).hexdigest(), work_icc_sha='a' * 64,
            scratch_root=tmp_path, scratch_bytes=1 << 20,
            max_file_bytes=1 << 20, qualify_runtime=lambda *_args: False,
            check_active=lambda: None)
    for builder in (lambda *_args: None,
                    lambda *_args: type('Foreign', (), {'connection': read})(),
                    foreign):
        with pytest.raises(ValueError, match='BUILDER_RESULT'):
            build_interchange_import_adopter(builder, read, Event())
    with pytest.raises(ValueError, match='BUILDER'):
        build_interchange_import_adopter(None, read, Event())
    with pytest.raises(ValueError, match='BUILDER'):
        build_interchange_import_adopter(lambda *_args: None, read, object())
