"""Real RSA math with fake token sessions; no hardware qualification."""
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace

import pytest


def installation(mode='valid'):
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import rsa, padding
    from cryptography.x509.oid import NameOID
    from photo_worker.operations.provenance.pkcs11_key import PKCS11RSAKey
    private = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'unit fixture')])
    now = datetime.now(timezone.utc)
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
        .public_key(private.public_key()).serial_number(1)
        .not_valid_before(now-timedelta(days=2))
        .not_valid_after(now+timedelta(days=1) if mode != 'expired' else now-timedelta(days=1))
        .sign(private, hashes.SHA256()).public_bytes(serialization.Encoding.PEM))
    attr = SimpleNamespace(SIGN='sign', SENSITIVE='sensitive', EXTRACTABLE='extractable')
    mechanisms = {name: name for name in ('SHA256','SHA384','SHA512',
        'SHA256_RSA_PKCS_PSS','SHA384_RSA_PKCS_PSS','SHA512_RSA_PKCS_PSS')}
    api = SimpleNamespace(Attribute=attr, Mechanism=SimpleNamespace(**mechanisms),
        MGF=SimpleNamespace(SHA256='mgf256',SHA384='mgf384',SHA512='mgf512'),
        ObjectClass=SimpleNamespace(PRIVATE_KEY='private'),KeyType=SimpleNamespace(RSA='rsa'))
    calls, closed = [], []
    class Key:
        def __getitem__(self, key):
            return {'sign': True, 'sensitive': mode != 'unsafe', 'extractable': False}[key]
        def sign(self, payload, *, mechanism, mechanism_param):
            bits = mechanism[3:6]
            digest = {'256': hashes.SHA256,'384': hashes.SHA384,'512': hashes.SHA512}[bits]()
            assert mechanism_param == ('SHA'+bits, 'mgf'+bits, digest.digest_size)
            calls.append('sign')
            if mode == 'bad_signature':
                return b'\0'*256
            return private.sign(payload, padding.PSS(mgf=padding.MGF1(digest), salt_length=digest.digest_size),digest)
    def get_key(**kwargs):
        assert kwargs == dict(object_class='private',key_type='rsa',id=b'key-id')
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
    provider = PKCS11RSAKey(api, SimpleNamespace(get_token=get_token),key_handle='handle',
        token_serial=b'serial',key_id=b'key-id',certificate_chain=cert,pin_supplier=lambda:'fixture-pin')
    return provider,calls,closed


@pytest.mark.parametrize('algorithm',['PS256','PS384','PS512'])
def test_pss_signature_matches_certificate_and_closes_session(algorithm):
    provider,calls,closed=installation()
    assert len(provider.sign_payload('handle',algorithm,b'payload')) == 256
    assert calls == ['token','sign'] and closed == [True]


@pytest.mark.parametrize('mode',['unsafe','bad_signature','wrong_token','expired'])
def test_invalid_key_token_signature_or_expiry_refused(mode):
    from cryptography.exceptions import InvalidSignature
    provider,calls,closed=installation(mode)
    with pytest.raises((ValueError,InvalidSignature)):
        provider.sign_payload('handle','PS256',b'payload')
    assert closed == ([True] if mode in ('unsafe','bad_signature') else [])


@pytest.mark.parametrize('handle,algorithm,payload',[
    ('wrong','PS256',b'x'),('handle','ES256',b'x'),('handle','PS256',b''),
    ('handle','PS256',bytearray(b'x'))])
def test_invalid_request_does_not_touch_token(handle,algorithm,payload):
    provider,calls,closed=installation()
    with pytest.raises(ValueError,match='SIGNER_PKCS11_REQUEST'):
        provider.sign_payload(handle,algorithm,payload)
    assert calls == closed == []


def test_live_key_challenge_uses_real_signature_and_closes():
    provider,calls,closed=installation()
    # Test fixture retains parsed certs; use that exact deployment chain.
    from cryptography.hazmat.primitives import serialization
    cert=provider._certificates[0].public_bytes(serialization.Encoding.PEM)
    assert provider.check_available('handle','PS256',cert) is True
    assert calls == ['token','sign'] and closed == [True]


def test_live_key_challenges_are_fresh_and_domain_separated(monkeypatch):
    provider,_,_=installation()
    from cryptography.hazmat.primitives import serialization
    cert=provider._certificates[0].public_bytes(serialization.Encoding.PEM)
    seen=[]
    monkeypatch.setattr(provider,'sign_payload',lambda *args:seen.append(args))
    for _ in range(2):
        assert provider.check_available('handle','PS384',cert) is True
    prefix=b'PhotoEditor/V31/PKCS11/key-availability/v1\x00'
    assert all(call[:2] == ('handle','PS384') and call[2].startswith(prefix)
               and len(call[2]) == len(prefix)+32 for call in seen)
    assert seen[0][2] != seen[1][2]


def test_live_key_wrong_certificate_does_not_sign(monkeypatch):
    provider,_,_=installation()
    monkeypatch.setattr(provider,'sign_payload',lambda *args:pytest.fail('key reached'))
    with pytest.raises(ValueError,match='SIGNER_PKCS11_CERTIFICATE_BINDING'):
        provider.check_available('handle','PS256',b'wrong-certificate')
