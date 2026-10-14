"""Real Ed25519 math with fake token sessions; no hardware qualification."""
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace

import pytest


def installation(mode='valid'):
    from cryptography import x509
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import ed25519
    from cryptography.x509.oid import NameOID
    from photo_worker.operations.provenance.pkcs11_ed25519_key import PKCS11Ed25519Key
    private = ed25519.Ed25519PrivateKey.generate()
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'unit fixture')])
    now = datetime.now(timezone.utc)
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
        .public_key(private.public_key()).serial_number(1)
        .not_valid_before(now-timedelta(days=2))
        .not_valid_after(now+timedelta(days=1) if mode != 'expired' else now-timedelta(days=1))
        .sign(private, None).public_bytes(serialization.Encoding.PEM))
    attr = SimpleNamespace(SIGN='sign', SENSITIVE='sensitive', EXTRACTABLE='extractable')
    api = SimpleNamespace(Attribute=attr, Mechanism=SimpleNamespace(EDDSA='eddsa'),
        ObjectClass=SimpleNamespace(PRIVATE_KEY='private'),KeyType=SimpleNamespace(EC_EDWARDS='edwards'))
    calls, closed = [], []
    class Key:
        def __getitem__(self, key):
            return {'sign': True, 'sensitive': mode != 'unsafe', 'extractable': False}[key]
        def sign(self, payload, *, mechanism):
            assert mechanism == 'eddsa'
            calls.append(payload)
            if mode == 'bad_signature': return b'\0'*64
            if mode == 'short_signature': return b'\0'*63
            return private.sign(payload)
    def get_key(**kwargs):
        assert kwargs == dict(object_class='private',key_type='edwards',id=b'key-id')
        return Key()
    @contextmanager
    def open_session(**kwargs):
        assert kwargs == dict(rw=False,user_pin='fixture-pin')
        try:
            yield SimpleNamespace(get_key=get_key)
        finally:
            closed.append(True)
    token = SimpleNamespace(serial=b'wrong' if mode == 'wrong_token' else b'serial',open=open_session)
    def get_token(**kwargs):
        calls.append('token')
        assert kwargs == dict(token_serial=b'serial')
        return token
    provider = PKCS11Ed25519Key(api, SimpleNamespace(get_token=get_token),key_handle='handle',
        token_serial=b'serial',key_id=b'key-id',certificate_chain=cert,pin_supplier=lambda:'fixture-pin')
    return provider,calls,closed


def test_ed25519_signs_exact_original_payload_and_closes_session():
    provider, calls, closed = installation()
    signature = provider.sign_payload('handle', 'ED25519', b'original-payload')
    provider._public.verify(signature, b'original-payload')
    assert len(signature) == 64
    assert calls == ['token', b'original-payload'] and closed == [True]


@pytest.mark.parametrize('mode', ['unsafe', 'bad_signature', 'short_signature', 'wrong_token', 'expired'])
def test_invalid_key_token_signature_or_expiry_refused(mode):
    from cryptography.exceptions import InvalidSignature
    provider, calls, closed = installation(mode)
    with pytest.raises((ValueError, InvalidSignature)):
        provider.sign_payload('handle', 'ED25519', b'payload')
    assert closed == ([True] if mode in ('unsafe', 'bad_signature', 'short_signature') else [])


@pytest.mark.parametrize('algorithm', ['PS256', 'ES256', 'Ed25519', None])
def test_other_algorithm_rejected_before_io(algorithm):
    provider, calls, closed = installation()
    with pytest.raises(ValueError, match='SIGNER_PKCS11_REQUEST'):
        provider.sign_payload('handle', algorithm, b'payload')
    assert calls == closed == []


def test_live_challenge_is_certificate_bound_and_fresh():
    from cryptography.hazmat.primitives import serialization
    provider, calls, closed = installation()
    cert = provider._certificates[0].public_bytes(serialization.Encoding.PEM)
    for _ in range(2):
        assert provider.check_available('handle', 'ED25519', cert) is True
    prefix = b'PhotoEditor/V31/PKCS11/key-availability/v1\x00'
    assert calls[1].startswith(prefix) and calls[3].startswith(prefix) and calls[1] != calls[3]
    assert closed == [True, True]
    with pytest.raises(ValueError, match='SIGNER_PKCS11_CERTIFICATE_BINDING'):
        provider.check_available('handle', 'ED25519', b'wrong')
