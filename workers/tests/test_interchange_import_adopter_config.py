"""Pinned V30 adopter resources cannot manufacture runtime qualification."""

from hashlib import sha256
from threading import Event
from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
from photo_worker.v2 import interchange_import_adopter_config as config_module


def environment(tmp_path):
    schema = canonical_bytes({'$defs': {}})
    schema_file = tmp_path / 'schemas.json'
    schema_file.write_bytes(schema)
    icc = tmp_path / 'icc'
    icc.mkdir()
    scratch = tmp_path / 'scratch'
    scratch.mkdir()
    values = {
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_ENABLED': 'true',
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_SCHEMAS_FILE': str(schema_file),
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_SCHEMAS_SHA': sha256(schema).hexdigest(),
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_SCRATCH_ROOT': str(scratch),
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_SCRATCH_BYTES': '1048576',
        'PHOTO_ADOPTER_INTERCHANGE_IMPORT_MAX_FILE_BYTES': '524288',
        'PHOTO_ICC_ROOT': str(icc),
    }
    return values, schema_file


def test_import_adopter_resources_pin_schemas_and_require_runtime(tmp_path, monkeypatch):
    values, _ = environment(tmp_path)
    monkeypatch.setattr(config_module, 'ICCRegistry', lambda _root: SimpleNamespace(
        resolve=lambda _name: SimpleNamespace(sha256=WORKING_PROFILE_SHA)))
    resources = config_module.import_adopter_resources_from_environment(values.get)
    assert resources.schemas_sha == values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_SCHEMAS_SHA']
    with pytest.raises(ValueError, match='RUNTIME_REQUIRED'):
        resources.builder(qualify_runtime=None)
    connection = SimpleNamespace(autocommit=True)
    installed = resources.builder(qualify_runtime=lambda *_args: False)(connection, Event())
    assert installed.connection is connection
    assert installed.work_icc_sha == WORKING_PROFILE_SHA


def test_import_adopter_resources_reject_pin_drift_partial_and_budget(tmp_path, monkeypatch):
    values, schema_file = environment(tmp_path)
    monkeypatch.setattr(config_module, 'ICCRegistry', lambda _root: SimpleNamespace(
        resolve=lambda _name: SimpleNamespace(sha256=WORKING_PROFILE_SHA)))
    schema_file.write_bytes(canonical_bytes({'$defs': {'changed': {}}}))
    with pytest.raises(ValueError, match='SCHEMA_PIN'):
        config_module.import_adopter_resources_from_environment(values.get)
    values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_ENABLED'] = 'false'
    with pytest.raises(ValueError, match='PARTIAL_CONFIG'):
        config_module.import_adopter_resources_from_environment(values.get)
    values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_ENABLED'] = 'true'
    values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_SCHEMAS_SHA'] = sha256(schema_file.read_bytes()).hexdigest()
    values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_MAX_FILE_BYTES'] = '1048577'
    with pytest.raises(ValueError, match='BUDGET'):
        config_module.import_adopter_resources_from_environment(values.get)


def test_import_adopter_resources_disabled_without_declaration():
    assert config_module.import_adopter_resources_from_environment({}.get) is None
