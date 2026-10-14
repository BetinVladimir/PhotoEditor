"""Pinned V30 adopter resources cannot manufacture runtime qualification."""

from hashlib import sha256
from threading import Event
from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
from photo_worker.v2 import interchange_import_adopter_config as config_module
from photo_worker.v2.interchange_import_adoption_runtime import SchedulerImportAdoptionRuntimeAuthority
from photo_worker.v2.interchange_import_adopter_config import (
    qualified_import_adopter_builder, import_adopter_operator_from_environment)
from test_interchange_import_adoption_qualification_report import fixture as report_fixture
from photo_worker.v2.interchange_import_adoption_runtime import ImportAdoptionQualification
from uuid import uuid4


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
    def runtime_builder(current, _stop):
        assert current is connection
        return SchedulerImportAdoptionRuntimeAuthority(
            SimpleNamespace(verified_report=lambda *_args: None),
            SimpleNamespace(get_object=lambda *_args, **_kwargs: None),
            {}, lambda *_args: False, lambda: None)
    installed = resources.builder(runtime_builder=runtime_builder)(connection, Event())
    assert type(installed.runtime) is SchedulerImportAdoptionRuntimeAuthority


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


def test_import_adopter_qualified_builder_binds_sql_object_and_person_authority(tmp_path, monkeypatch):
    values, _ = environment(tmp_path)
    monkeypatch.setattr(config_module, 'ICCRegistry', lambda _root: SimpleNamespace(
        resolve=lambda _name: SimpleNamespace(sha256=WORKING_PROFILE_SHA)))
    resources = config_module.import_adopter_resources_from_environment(values.get)
    expected, _ = report_fixture()
    expected['Operation'], expected['ModelLockSHA'] = 'mask.person', 'd' * 64
    selected = {'mask.person': ImportAdoptionQualification(expected, uuid4(), 'a' * 64)}
    objects = SimpleNamespace(get_object=lambda *_args, **_kwargs: None)
    connection = SimpleNamespace(autocommit=True)
    builder = qualified_import_adopter_builder(resources, selected, objects,
        'photo', lambda *_args: False)
    installed = builder(connection, Event())
    assert installed.runtime.catalog.connection is connection
    assert installed.runtime.objects is objects
    assert installed.runtime.person_current.connection is connection
    with pytest.raises(ValueError, match='OPERATOR_INSTALLATION'):
        qualified_import_adopter_builder(resources, selected, objects, 'photo', None)


def test_import_adopter_operator_environment_requires_both_pins(tmp_path, monkeypatch):
    values, _ = environment(tmp_path)
    monkeypatch.setattr(config_module, 'ICCRegistry', lambda _root: SimpleNamespace(
        resolve=lambda _name: SimpleNamespace(sha256=WORKING_PROFILE_SHA)))
    with pytest.raises(ValueError, match='PARTIAL_CONFIG'):
        import_adopter_operator_from_environment(values.get)
    expected, _ = report_fixture()
    manifest = canonical_bytes({'schema_version': 1, 'qualifications': [{
        'operation': 'image.denoise', 'scale': None, 'expected': expected,
        'report_id': str(uuid4()), 'report_sha': 'a' * 64}]})
    path = tmp_path / 'qualifications.json'
    path.write_bytes(manifest)
    values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_QUALIFICATIONS_FILE'] = str(path)
    values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_QUALIFICATIONS_SHA'] = sha256(manifest).hexdigest()
    operator = import_adopter_operator_from_environment(values.get)
    objects = SimpleNamespace(get_object=lambda *_args, **_kwargs: None)
    builder = operator(objects, 'photo')
    installed = builder(SimpleNamespace(autocommit=True), Event())
    assert installed.runtime.objects is objects
    assert installed.runtime.policies['image.denoise'].expected == expected
    values['PHOTO_ADOPTER_INTERCHANGE_IMPORT_ENABLED'] = 'false'
    for key in ('SCHEMAS_FILE', 'SCHEMAS_SHA', 'SCRATCH_ROOT',
                'SCRATCH_BYTES', 'MAX_FILE_BYTES'):
        values.pop('PHOTO_ADOPTER_INTERCHANGE_IMPORT_' + key)
    with pytest.raises(ValueError, match='PARTIAL_CONFIG'):
        import_adopter_operator_from_environment(values.get)
