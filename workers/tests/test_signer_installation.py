"""Signer composition boundaries, with explicit native and SQL doubles."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy
from photo_worker.runtime.signer_installation import build_signer_executor


def policy_bytes():
    return canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha='a'*64, key_handle='fixture', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha='c'*64, revocation_mode='off'))


@pytest.mark.parametrize('mode', ['valid', 'unregistered', 'revoked_during_native',
                                 'native_denied', 'native_truthy', 'mixed_operations'])
def test_composition_requires_both_live_authorities(mode):
    raw = policy_bytes()
    worker = uuid4()
    qualification = 'd'*64
    live = mode != 'unregistered'
    calls = []
    def registration(w, sdk, certificate, trust, report):
        assert (w, sdk, certificate, trust, report) == (worker, 'a'*64, 'b'*64, 'c'*64, qualification)
        calls.append('registration')
        return live
    def native(policy, digest, report):
        nonlocal live
        assert digest == sha256(raw).hexdigest() and report == qualification
        assert canonical_bytes(policy) == raw
        calls.append('native')
        if mode == 'revoked_during_native':
            live = False
        if mode == 'native_denied':
            return False
        if mode == 'native_truthy':
            return 1
        return True
    operations = frozenset({'provenance.sign', 'metadata.write'}) if mode == 'mixed_operations' else frozenset({'provenance.sign'})
    database = SimpleNamespace(signer_runtime_ready=registration,
                               runtime_operations=lambda *args: operations)
    delegate = SimpleNamespace(check_installation=lambda _: calls.append('delegate'))
    def build():
        return build_signer_executor(database, worker, None, [raw], qualification, native, delegate)
    if mode != 'valid':
        with pytest.raises(ValueError):
            build()
        assert 'delegate' not in calls
    else:
        executor = build()
        assert set(executor.operations) == {'provenance.sign'}
        assert executor.worker_id == worker
        assert calls.count('native') == 2 and calls.count('registration') == 4


def test_mixed_native_installation_and_duplicate_policy_refused():
    raw = policy_bytes()
    other = SigningPolicy.model_validate_json(policy_bytes())
    other.sdk_lock_sha = 'e'*64
    db = SimpleNamespace(runtime_operations=lambda *args: pytest.fail('mixed pins reached SQL'))
    for policies in ([raw, canonical_bytes(other)], [raw, raw]):
        with pytest.raises(ValueError):
            build_signer_executor(db, uuid4(), None, policies, 'd'*64, lambda *args: True, object())


def test_native_composition_preserves_independent_registration(monkeypatch):
    import photo_worker.runtime.signer_installation as installation
    pem = b'-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n'
    policy = SigningPolicy.model_validate_json(policy_bytes())
    policy.certificate_chain_sha = policy.trust_bundle_sha = sha256(pem).hexdigest()
    raw = canonical_bytes(policy)
    worker = uuid4()
    database = SimpleNamespace(signer_runtime_ready=lambda *args: True,
        runtime_operations=lambda *args: frozenset({'provenance.sign'}))
    calls = []
    def inspect(candidate, algorithm, certificate):
        calls.append('inspect')
        assert candidate == policy and algorithm == 'ES256' and certificate == pem
        return True
    executor = installation.build_native_signer_executor(database, worker, None, [raw],
        'd'*64, lambda *args: True, sdk=object(), trust_bundle=pem,
        certificate_chain=pem, algorithm='ES256', sign_payload=lambda *args: b'signature',
        inspect_installation=inspect, verify_binding=lambda *args: True)
    assert executor.worker_id == worker
    assert set(executor.operations) == {'provenance.sign'}
    assert calls == ['inspect']
    database.signer_runtime_ready = lambda *args: False
    with pytest.raises(ValueError):
        installation.build_native_signer_executor(database, worker, None, [raw],
            'd'*64, lambda *args: True, sdk=object(), trust_bundle=pem,
            certificate_chain=pem, algorithm='ES256', sign_payload=lambda *args: b'signature',
            inspect_installation=inspect, verify_binding=lambda *args: True)
    assert calls == ['inspect']


@pytest.mark.parametrize('mode',['algorithm','key_handle'])
def test_pkcs11_process_rejects_policy_key_or_algorithm_before_native(mode):
    import photo_worker.runtime.signer_installation as installation
    raw=policy_bytes()
    with pytest.raises(ValueError,match='SIGNER_PROCESS_PKCS11_'):
        installation.build_pkcs11_signer_executor(object(),uuid4(),None,[raw],'d'*64,
            lambda *args:True,sdk=object(),trust_bundle=b'not-read',
            certificate_chain=b'not-read',algorithm='unsupported' if mode=='algorithm' else 'PS256',
            pkcs11=object(),library=object(),key_handle='wrong' if mode=='key_handle' else 'fixture',
            token_serial=b'serial',key_id=b'id',pin_supplier=lambda:pytest.fail('PIN read'),
            native_inspector=lambda *args:True,verify_binding=lambda *args:True)


@pytest.mark.parametrize('algorithm', ['ES256', 'ES384', 'ES512', 'ED25519'])
def test_pkcs11_factory_selects_ec_or_ed_provider(monkeypatch, algorithm):
    from types import SimpleNamespace
    import photo_worker.runtime.signer_installation as installation
    import photo_worker.operations.provenance.pkcs11_ec_key as ec_provider
    import photo_worker.operations.provenance.pkcs11_key as rsa_provider
    import photo_worker.operations.provenance.pkcs11_ed25519_key as ed_provider
    calls = []
    key = SimpleNamespace(sign_payload=lambda *args: b'signature', check_available=lambda *args: True)
    def build_key(*args, **kwargs):
        calls.append(kwargs)
        return key
    monkeypatch.setattr(ec_provider, 'PKCS11ECKey', build_key if algorithm.startswith('ES') else lambda *a, **k: pytest.fail('EC provider selected'))
    monkeypatch.setattr(ed_provider, 'PKCS11Ed25519Key', build_key if algorithm == 'ED25519' else lambda *a, **k: pytest.fail('Ed25519 provider selected'))
    monkeypatch.setattr(rsa_provider, 'PKCS11RSAKey', lambda *a, **k: pytest.fail('RSA provider selected'))
    expected = object()
    def native(*args, **kwargs):
        assert kwargs['algorithm'] == algorithm
        assert kwargs['sign_payload'] is key.sign_payload
        return expected
    monkeypatch.setattr(installation, 'build_native_signer_executor', native)
    result = installation.build_pkcs11_signer_executor(object(), uuid4(), None, [policy_bytes()], 'd'*64,
        lambda *args: True, sdk=object(), trust_bundle=b'trust', certificate_chain=b'certificate',
        algorithm=algorithm, pkcs11=object(), library=object(), key_handle='fixture',
        token_serial=b'serial', key_id=b'id', pin_supplier=lambda: pytest.fail('PIN read'),
        native_inspector=lambda *args: True, verify_binding=lambda *args: True)
    assert result is expected and len(calls) == 1
    assert calls[0]['key_handle'] == 'fixture'


@pytest.mark.parametrize('registered', [True, False])
def test_operator_handoff_occurs_only_after_complete_installation(registered):
    from photo_worker.operations.provenance.installation import InstalledSigningOperator
    raw, worker, captured = policy_bytes(), uuid4(), []
    database = SimpleNamespace(signer_runtime_ready=lambda *args: registered,
        runtime_operations=lambda *args: frozenset({'provenance.sign'}))
    delegate = SimpleNamespace(check_installation=lambda _: None)
    def build():
        return build_signer_executor(database, worker, None, [raw], 'd'*64,
            lambda *args: True, delegate, on_operator=captured.append)
    if registered:
        executor = build()
        assert len(captured) == 1 and isinstance(captured[0], InstalledSigningOperator)
        assert executor.database is database
        captured[0].check_installation(SigningPolicy.model_validate_json(raw))
    else:
        with pytest.raises(ValueError):build()
        assert captured == []


def test_operator_handoff_failure_refuses_executor():
    database = SimpleNamespace(signer_runtime_ready=lambda *args: True,
        runtime_operations=lambda *args: frozenset({'provenance.sign'}))
    def reject(operator):raise RuntimeError('listener composition refused')
    with pytest.raises(RuntimeError, match='composition refused'):
        build_signer_executor(database, uuid4(), None, [policy_bytes()], 'd'*64,
            lambda *args: True, SimpleNamespace(check_installation=lambda _: None),
            on_operator=reject)


@pytest.mark.parametrize('change_at', ['before_call', 'qualification', 'native', 'delegate'])
@pytest.mark.parametrize('replacement', [frozenset(), frozenset({'provenance.sign', 'metadata.write'})])
def test_live_signer_operation_isolation_survives_initial_installation(change_at, replacement):
    raw, captured, calls = policy_bytes(), [], []
    operations = frozenset({'provenance.sign'})
    armed = False
    def change(stage):
        nonlocal operations
        if armed and change_at == stage:
            operations = replacement
    def registered(*args):
        calls.append('qualification')
        change('qualification')
        return True
    def native(*args):
        calls.append('native')
        change('native')
        return True
    def delegate_check(policy):
        calls.append('delegate')
        change('delegate')
    database = SimpleNamespace(signer_runtime_ready=registered,
        runtime_operations=lambda *args: operations)
    build_signer_executor(database, uuid4(), None, [raw], 'd'*64, native,
        SimpleNamespace(check_installation=delegate_check), on_operator=captured.append)
    assert len(captured) == 1
    calls.clear()
    armed = True
    change('before_call')
    with pytest.raises(ValueError, match='SIGNER_INSTALLATION_'):
        captured[0].check_installation(SigningPolicy.model_validate_json(raw))
    if change_at == 'before_call':
        assert calls == []
    elif change_at == 'qualification':
        assert calls == ['qualification']
    elif change_at == 'native':
        assert calls == ['qualification', 'native']
    else:
        assert calls == ['qualification', 'native', 'qualification', 'delegate']
