from photo_worker.contracts.base import canonical_bytes
"""Native composition doubles only; not real certificate/SDK qualification."""
from contextlib import contextmanager
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.provenance import SigningPolicy
from photo_worker.operations.provenance.native_operator import NativeSigningDelegate

PEM = b'-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n'


def setup_delegate(*, inspect=None, sign=None, verify=None, timestamp=False):
    handles, closed, signatures = [], [], []
    @contextmanager
    def create(callback, algorithm, certificate, tsa):
        assert algorithm == 'algorithm-es256' and certificate == PEM.decode()
        assert tsa == ('https://tsa.example.com/sign' if timestamp else None)
        handle = SimpleNamespace(callback=callback)
        handles.append(handle)
        try:
            yield handle
        finally:
            closed.append(True)
    sdk = SimpleNamespace(Signer=SimpleNamespace(from_callback=create),
                          C2paSigningAlg=SimpleNamespace(ES256='algorithm-es256'))
    def default_sign(key, algorithm, payload):
        signatures.append((key, algorithm, payload))
        return b'signature'
    delegate = NativeSigningDelegate(sdk, 'a'*64, PEM, PEM, 'ES256', sign or default_sign,
        inspect or (lambda *args: True), verify or (lambda *args: True),
        {'tsa': 'https://tsa.example.com/sign'} if timestamp else {}, ['tsa.example.com'])
    policy = SigningPolicy.model_validate_json(canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha='a'*64, key_handle='external-key', certificate_chain_sha=sha256(PEM).hexdigest(),
        timestamp_mode='required' if timestamp else 'off', timestamp_service_id='tsa' if timestamp else None,
        allowed_mime=['image/png'], trust_bundle_sha=sha256(PEM).hexdigest(), revocation_mode='off')))
    return delegate, policy, handles, closed, signatures


@pytest.mark.parametrize('timestamp', [False, True])
def test_external_key_and_callback_lifetime(timestamp):
    delegate, policy, handles, closed, signatures = setup_delegate(timestamp=timestamp)
    with delegate.open_signer(policy) as signer:
        assert signer.callback(b'payload') == b'signature'
    assert signatures == [('external-key', 'ES256', b'payload')]
    assert closed == [True]
    with pytest.raises(ValueError, match='SIGNER_NATIVE_PAYLOAD'):
        handles[0].callback(b'payload')
    assert len(signatures) == 1


@pytest.mark.parametrize('signature', [None, '', bytearray(b'x'), b'', b'x'*16385])
def test_invalid_signature_refused(signature):
    delegate, policy, _, closed, _ = setup_delegate(sign=lambda *args: signature)
    with pytest.raises(ValueError, match='SIGNER_NATIVE_SIGNATURE'):
        with delegate.open_signer(policy) as signer:
            signer.callback(b'payload')
    assert closed == [True]


def test_revocation_during_key_call_refused():
    live = True
    def sign(*args):
        nonlocal live
        live = False
        return b'signature'
    delegate, policy, _, closed, _ = setup_delegate(inspect=lambda *args: live, sign=sign)
    with pytest.raises(ValueError, match='SIGNER_NATIVE_UNQUALIFIED'):
        with delegate.open_signer(policy) as signer:
            signer.callback(b'payload')
    assert closed == [True]


@pytest.mark.parametrize('field,value', [('sdk_lock_sha', 'b'*64),
    ('trust_bundle_sha', 'b'*64), ('certificate_chain_sha', 'b'*64),
    ('timestamp_service_id', 'unknown')])
def test_policy_pins_and_timestamp_service_checked_before_handle(field, value):
    delegate, policy, handles, _, _ = setup_delegate(timestamp=True)
    with pytest.raises(ValueError, match='SIGNER_NATIVE_POLICY'):
        with delegate.open_signer(policy.model_copy(update={field: value})):
            pytest.fail('policy accepted')
    assert handles == []


@pytest.mark.parametrize('answer', [False, None, 1, 'verified'])
def test_independent_binding_requires_strict_attestation(answer):
    delegate, policy, _, _, _ = setup_delegate(verify=lambda *args: answer)
    lineage = SimpleNamespace(model_copy=lambda **kw: object())
    # canonical_bytes expects a JSON value or Contract; use the real contract.
    from photo_worker.contracts.provenance import SigningLineage
    lineage = SigningLineage(schema_version=1, source_hashes=['a'*64], actions=[])
    with pytest.raises(ValueError, match='SIGNER_NATIVE_BINDING'):
        delegate.verify_reader(object(), policy, lineage, b'manifest')


@pytest.mark.parametrize('url', ['http://tsa.example.com', 'https://other.example.com',
    'https://user:secret@tsa.example.com', 'https://tsa.example.com:444',
    'https://tsa.example.com/#fragment'])
def test_timestamp_deployment_urls_restricted(url):
    with pytest.raises(ValueError, match='SIGNER_NATIVE_TIMESTAMP'):
        NativeSigningDelegate(object(), 'a'*64, PEM, PEM, 'ES256', lambda *a: b'x',
            lambda *a: True, lambda *a: True, {'tsa': url}, ['tsa.example.com'])


@pytest.mark.parametrize('mode', ['valid', 'policy', 'domain', 'nonce', 'handle', 'algorithm', 'extra', 'revoked'])
def test_operator_challenge_is_closed_policy_bound_and_rechecks_native_authority(mode):
    from photo_worker.contracts.base import canonical_bytes
    live = True
    def sign(*args):
        nonlocal live
        if mode == 'revoked': live = False
        return b'proof'
    delegate, policy, _, _, _ = setup_delegate(inspect=lambda *args: live, sign=sign)
    body = dict(domain='photoeditor.signing-operator-possession/v1',
                policy_sha256=sha256(canonical_bytes(policy)).hexdigest(), nonce='b'*64)
    if mode == 'policy': body['policy_sha256'] = 'f'*64
    if mode == 'domain': body['domain'] = 'arbitrary-signature'
    if mode == 'nonce': body['nonce'] = 'short'
    if mode == 'extra': body['extra'] = True
    handle = 'foreign' if mode == 'handle' else policy.key_handle
    algorithm = 'ED25519' if mode == 'algorithm' else 'ES256'
    if mode == 'valid':
        assert delegate.sign_operator_challenge(policy, handle, algorithm, canonical_bytes(body)) == b'proof'
    else:
        with pytest.raises(ValueError):
            delegate.sign_operator_challenge(policy, handle, algorithm, canonical_bytes(body))


def test_inflight_native_callback_cannot_return_signature_after_handle_closes():
    entered, release = Event(), Event()
    key_calls = []

    def sign(key, algorithm, payload):
        key_calls.append((key, algorithm, payload))
        entered.set()
        if not release.wait(2):
            raise AssertionError('callback was not released')
        return b'signature'

    delegate, policy, _, closed, _ = setup_delegate(sign=sign)
    with ThreadPoolExecutor(max_workers=1) as pool:
        try:
            with delegate.open_signer(policy) as signer:
                future = pool.submit(signer.callback, b'payload')
                assert entered.wait(2)
            assert closed == [True]
            release.set()
            with pytest.raises(ValueError, match='SIGNER_NATIVE_PAYLOAD'):
                future.result(timeout=2)
        finally:
            release.set()
    assert key_calls == [('external-key', 'ES256', b'payload')]
