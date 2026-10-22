from photo_worker.contracts.base import canonical_bytes
"""Settings construction tests; no native qualification evidence."""
from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.provenance import SigningPolicy
from photo_worker.operations.provenance.native_context import PinnedSigningContext

TRUST = b'-----BEGIN CERTIFICATE-----\nfixture-not-a-real-certificate\n-----END CERTIFICATE-----\n'


def policy():
    return SigningPolicy.model_validate_json(canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha='a'*64, key_handle='fixture', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha=sha256(TRUST).hexdigest(), revocation_mode='off')))


def test_fresh_pinned_settings_and_cleanup():
    settings_seen, closed = [], []
    @contextmanager
    def create(settings):
        settings_seen.append(settings)
        try:
            yield object()
        finally:
            closed.append(True)
    factory = PinnedSigningContext(SimpleNamespace(Context=SimpleNamespace(from_dict=create)),
                                   'a'*64, TRUST, ['ocsp.example.com'])
    for _ in range(2):
        with factory.open_context(policy()):
            pass
    first, second = settings_seen
    assert first == second and first is not second
    assert first['trust'] == {'trust_anchors': TRUST.decode('ascii')}
    assert first['verify']['verify_trust'] is True
    assert first['verify']['verify_timestamp_trust'] is True
    assert first['verify']['remote_manifest_fetch'] is False
    assert first['verify']['ocsp_fetch'] is False
    assert first['signer'] is None
    first['core']['allowed_network_hosts'].append('mutated.example.com')
    assert second['core']['allowed_network_hosts'] == ['ocsp.example.com']
    assert closed == [True, True]


@pytest.mark.parametrize('field', ['sdk_lock_sha', 'trust_bundle_sha'])
def test_policy_pin_mismatch_before_native_call(field):
    factory = PinnedSigningContext(object(), 'a'*64, TRUST)
    candidate = policy().model_copy(update={field: 'c'*64})
    with pytest.raises(ValueError, match='SIGNER_CONTEXT_POLICY'):
        with factory.open_context(candidate):
            pytest.fail('unqualified context exposed')


@pytest.mark.parametrize('hosts', [['*'], ['https://ocsp.example.com'], ['localhost:80'],
                                  ['UPPER.example.com'], ['a..b'], ['-a.example'],
                                  ['a.example', 'a.example']])
def test_network_allowlist_rejects_ambiguous_hosts(hosts):
    with pytest.raises(ValueError, match='SIGNER_CONTEXT_NETWORK'):
        PinnedSigningContext(object(), 'a'*64, TRUST, hosts)


def test_required_revocation_needs_network_and_enables_ocsp():
    candidate = policy().model_copy(update={'revocation_mode': 'required'})
    with pytest.raises(ValueError, match='SIGNER_CONTEXT_NETWORK_REQUIRED'):
        with PinnedSigningContext(object(), 'a'*64, TRUST).open_context(candidate):
            pytest.fail('missing network accepted')
    @contextmanager
    def create(settings):
        assert settings['verify']['ocsp_fetch'] is True
        yield object()
    sdk = SimpleNamespace(Context=SimpleNamespace(from_dict=create))
    with PinnedSigningContext(sdk, 'a'*64, TRUST, ['ocsp.example.com']).open_context(candidate):
        pass


def test_native_context_closes_on_consumer_failure():
    closed = []
    @contextmanager
    def create(settings):
        try:
            yield object()
        finally:
            closed.append(True)
    sdk = SimpleNamespace(Context=SimpleNamespace(from_dict=create))
    with pytest.raises(RuntimeError):
        with PinnedSigningContext(sdk, 'a'*64, TRUST).open_context(policy()):
            raise RuntimeError('consumer')
    assert closed == [True]


@pytest.mark.parametrize('trust', [b'', b'not-pem', b'\xff',
    TRUST + b'-----BEGIN PRIVATE KEY-----', b'x' * ((1 << 20) + 1)])
def test_invalid_trust_bundle_refused(trust):
    with pytest.raises(ValueError):
        PinnedSigningContext(object(), 'a'*64, trust)
