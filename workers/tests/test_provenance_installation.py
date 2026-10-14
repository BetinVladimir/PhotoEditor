"""Installation binding only; fake authority never proves native qualification."""
from contextlib import nullcontext
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.operations.provenance.execute import signer_operations


def policy_bytes():
    return canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha='a'*64, key_handle='fixture', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha='c'*64, revocation_mode='off'))


def test_installed_body_and_dependency_buffers_are_owned():
    raw = policy_bytes()
    policy = SigningPolicy.model_validate_json(raw)
    seen = []
    def authority(candidate, digest):
        assert canonical_bytes(candidate) == raw
        assert digest == sha256(raw).hexdigest()
        candidate.key_handle = 'mutated-authority-copy'
        return True
    def delegate_check(candidate):
        seen.append(canonical_bytes(candidate))
        candidate.key_handle = 'mutated-delegate-copy'
    delegate = SimpleNamespace(check_installation=delegate_check)
    installed = InstalledSigningOperator([raw], authority, delegate)
    installed.check_installation(policy)
    installed.check_installation(policy)
    assert seen == [raw, raw]
    assert canonical_bytes(policy) == raw
    with pytest.raises(ValueError):
        installed.check_installation(policy.model_copy(update={'key_handle': 'different'}))


@pytest.mark.parametrize('attestation', [False, None, 1, 'qualified'])
def test_no_truthy_or_revoked_authority_is_accepted(attestation):
    raw = policy_bytes()
    calls = []
    installed = InstalledSigningOperator([raw], lambda *args: attestation,
        SimpleNamespace(check_installation=lambda _: calls.append('delegate')))
    with pytest.raises(ValueError):
        installed.check_installation(SigningPolicy.model_validate_json(raw))
    assert calls == []


@pytest.mark.parametrize('stage', ['context', 'signer'])
def test_revocation_while_handle_is_open_closes_and_refuses(stage):
    raw = policy_bytes()
    policy = SigningPolicy.model_validate_json(raw)
    live = True
    closed = []
    from contextlib import contextmanager
    @contextmanager
    def handle(_):
        try:
            yield object()
        finally:
            closed.append(True)
    delegate = SimpleNamespace(check_installation=lambda _: None,
                               open_context=handle, open_signer=handle)
    installed = InstalledSigningOperator([raw], lambda *args: live, delegate)
    with pytest.raises(ValueError):
        with getattr(installed, 'open_' + stage)(policy):
            live = False
    assert closed == [True]


def test_duplicate_and_unbound_factory_are_refused():
    raw = policy_bytes()
    with pytest.raises(ValueError):
        InstalledSigningOperator([raw, raw], lambda *args: True, object())
    with pytest.raises(ValueError):
        signer_operations(None, object())


@pytest.mark.parametrize('stage', ['context', 'signer'])
@pytest.mark.parametrize('mutation_stage', ['open', 'close'])
def test_native_handle_policy_mutation_refused(stage, mutation_stage):
    from contextlib import contextmanager
    raw = policy_bytes()
    policy = SigningPolicy.model_validate_json(raw)
    closed, used = [], []
    @contextmanager
    def handle(candidate):
        try:
            if mutation_stage == 'open':
                candidate.key_handle = 'different-key'
            yield object()
        finally:
            if mutation_stage == 'close':
                candidate.key_handle = 'different-key'
            closed.append(True)
    delegate = SimpleNamespace(check_installation=lambda _: None,
                               open_context=handle, open_signer=handle)
    installed = InstalledSigningOperator([raw], lambda *args: True, delegate)
    with pytest.raises(ValueError, match='SIGNER_POLICY_NOT_INSTALLED'):
        with getattr(installed, 'open_' + stage)(policy):
            used.append(True)
    assert closed == [True]
    assert used == ([] if mutation_stage == 'open' else [True])
    assert canonical_bytes(policy) == raw


def test_reader_policy_mutation_refused():
    raw = policy_bytes()
    policy = SigningPolicy.model_validate_json(raw)
    def verify(reader, candidate, lineage, manifest):
        candidate.key_handle = 'different-key'
    installed = InstalledSigningOperator([raw], lambda *args: True,
        SimpleNamespace(check_installation=lambda _: None, verify_reader=verify))
    with pytest.raises(ValueError, match='SIGNER_POLICY_NOT_INSTALLED'):
        installed.verify_reader(object(), policy, SimpleNamespace(model_copy=lambda **kw: object()), b'{}')
    assert canonical_bytes(policy) == raw
