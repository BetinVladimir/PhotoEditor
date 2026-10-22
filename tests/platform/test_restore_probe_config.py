"""Essential configuration rejection and fixed-type construction scenarios."""
from dataclasses import asdict
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_probe_config as m
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


def test_config_does_not_accept_serialized_barrier_or_callback(tmp_path):
    with pytest.raises(ValueError, match='RESTORE_PROBE_CONFIG_INSTALLATION'):
        m.load_restore_probe_dispatch(tmp_path / 'config', '0' * 64,
            connection=object(), client=object(), replay=object(),
            renderer_barrier={'held': True}, assert_probe_isolation=lambda: True,
            check_active=lambda: True)


def test_fixed_type_unknown_and_missing_fields_rejected():
    with pytest.raises(ValueError, match='RESTORE_PROBE_CONFIG_FIELDS'):
        m._build(RestoreRevokeFixture, {'callable': 'anything'}, lambda: True)


def test_descriptor_pin_is_external_and_rechecked(tmp_path):
    path = tmp_path / 'source.json'
    path.write_bytes(b'{}'); path.chmod(0o600)
    descriptor = {'path': str(path), 'sha256': sha256(b'{}').hexdigest()}
    assert m._read_pin(descriptor, 1024, lambda: True) == b'{}'
    path.write_bytes(b'[]')
    with pytest.raises(ValueError, match='RESTORE_PROBE_CONFIG_FILE_PIN'):
        m._read_pin(descriptor, 1024, lambda: True)


def test_private_pin_refuses_public_secret_and_inactive_read(tmp_path):
    path = tmp_path / 'pepper'
    path.write_bytes(b'x' * 32); path.chmod(0o644)
    descriptor = {'path': str(path), 'sha256': sha256(path.read_bytes()).hexdigest()}
    with pytest.raises(ValueError, match='RESTORE_KEY_FILE_MODE'):
        m._read_pin(descriptor, 1024, lambda: True)
    path.chmod(0o600)
    with pytest.raises((ValueError, RuntimeError)):
        m._read_pin(descriptor, 1024, lambda: False)


def test_model_parser_rejects_boolean_integer_and_foreign_uuid():
    with pytest.raises(ValueError):
        m._value(int, True, lambda: True)
    with pytest.raises(ValueError):
        m._value(type(uuid4()), '00000000-0000-0000-0000-000000000000', lambda: True)


def test_nested_consent_target_constructs_real_typed_fixture():
    from photo_worker.runtime.restore_revoke_lifecycle import RestoreConsentRevokeTarget
    ids = {name: str(uuid4()) for name in ('tenant_id','project_id','consent_id','person_id',
                                          'actor_id','learning_version_id')}
    raw = {'origin': 'https://restore-api', 'idempotency_key': 'restore-revoke-one',
           'authorization': 'Bearer installed', 'fixture': {**ids,
               'purpose': 'learning', 'epoch': 1, 'revision': 2, 'policy_version': 'v1'}}
    value = m._build(RestoreConsentRevokeTarget, raw, lambda: True)
    assert isinstance(value, RestoreConsentRevokeTarget)
    assert isinstance(value.fixture, RestoreRevokeFixture)
    assert str(value.fixture.tenant_id) == ids['tenant_id']
    assert value.fixture.epoch == 1
