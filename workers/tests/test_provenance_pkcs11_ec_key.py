"""Real ECDSA math with fake token sessions; no hardware qualification."""
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace

import pytest


def installation(mode='valid', algorithm='ES256'):
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec, utils
    from cryptography.x509.oid import NameOID
    from photo_worker.operations.provenance.pkcs11_ec_key import PKCS11ECKey
    private = ec.generate_private_key({'ES256':ec.SECP256R1,'ES384':ec.SECP384R1,'ES512':ec.SECP521R1}[algorithm]())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'unit fixture')])
    now = datetime.now(timezone.utc)
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
        .public_key(private.public_key()).serial_number(1)
        .not_valid_before(now-timedelta(days=2))
        .not_valid_after(now+timedelta(days=1) if mode != 'expired' else now-timedelta(days=1))
        .sign(private, hashes.SHA256()).public_bytes(serialization.Encoding.PEM))
    attr = SimpleNamespace(SIGN='sign', SENSITIVE='sensitive', EXTRACTABLE='extractable')
    api = SimpleNamespace(Attribute=attr, Mechanism=SimpleNamespace(ECDSA='ecdsa'),
        ObjectClass=SimpleNamespace(PRIVATE_KEY='private'),KeyType=SimpleNamespace(EC='ec'))
    calls, closed = [], []
    class Key:
        def __getitem__(self, key):
            return {'sign': True, 'sensitive': mode != 'unsafe', 'extractable': False}[key]
        def sign(self, payload, *, mechanism):
            digest = {'ES256':hashes.SHA256,'ES384':hashes.SHA384,'ES512':hashes.SHA512}[algorithm]()
            assert mechanism == 'ecdsa' and len(payload) == digest.digest_size
            calls.append('sign')
            width = (private.key_size + 7) // 8
            if mode == 'bad_signature': return b'\0' * (2 * width)
            der = private.sign(payload, ec.ECDSA(utils.Prehashed(digest)))
            r, s = utils.decode_dss_signature(der)
            return r.to_bytes(width,'big') + s.to_bytes(width,'big')
    def get_key(**kwargs):
        assert kwargs == dict(object_class='private',key_type='ec',id=b'key-id')
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
    provider = PKCS11ECKey(api, SimpleNamespace(get_token=get_token),key_handle='handle',
        token_serial=b'serial',key_id=b'key-id',certificate_chain=cert,pin_supplier=lambda:'fixture-pin')
    return provider,calls,closed


@pytest.mark.parametrize('algorithm', ['ES256', 'ES384', 'ES512'])
def test_ec_signature_matches_certificate_and_returns_der(algorithm):
    from cryptography.hazmat.primitives import hashes
    from cryptography.hazmat.primitives.asymmetric import ec
    provider, calls, closed = installation(algorithm=algorithm)
    signature = provider.sign_payload('handle', algorithm, b'payload')
    digest = {'ES256':hashes.SHA256,'ES384':hashes.SHA384,'ES512':hashes.SHA512}[algorithm]()
    provider._public.verify(signature, b'payload', ec.ECDSA(digest))
    assert signature[0] == 0x30
    assert calls == ['token', 'sign'] and closed == [True]


@pytest.mark.parametrize('mode', ['unsafe', 'bad_signature', 'wrong_token', 'expired'])
def test_invalid_key_token_signature_or_expiry_refused(mode):
    from cryptography.exceptions import InvalidSignature
    provider, calls, closed = installation(mode)
    with pytest.raises((ValueError, InvalidSignature)):
        provider.sign_payload('handle', 'ES256', b'payload')
    assert closed == ([True] if mode in ('unsafe', 'bad_signature') else [])


def test_wrong_curve_algorithm_does_not_open_token():
    provider, calls, closed = installation()
    with pytest.raises(ValueError, match='SIGNER_PKCS11_EC_ALGORITHM'):
        provider.sign_payload('handle', 'ES384', b'payload')
    assert calls == closed == []


def test_live_ec_challenge_certificate_binding():
    from cryptography.hazmat.primitives import serialization
    provider, calls, closed = installation()
    cert = provider._certificates[0].public_bytes(serialization.Encoding.PEM)
    assert provider.check_available('handle', 'ES256', cert) is True
    assert calls == ['token','sign'] and closed == [True]
    with pytest.raises(ValueError, match='SIGNER_PKCS11_CERTIFICATE_BINDING'):
        provider.check_available('handle', 'ES256', b'wrong')
