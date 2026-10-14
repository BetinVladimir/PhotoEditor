from datetime import datetime, timedelta, timezone
from hashlib import sha256

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import serialization, hashes
from cryptography.hazmat.primitives.asymmetric import ed25519, ec, rsa
from cryptography.x509.oid import NameOID

from photo_worker.contracts.provenance import SigningPolicy
from photo_worker.operations.provenance.certificate_inspector import CertificateBoundInstallationInspector
from test_signer_deployment import fixture


def certificate(now, *, digital=True, key=None, ca=False, usage=True):
    key=key or ed25519.Ed25519PrivateKey.generate()
    name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'isolated signer test')])
    builder=(x509.CertificateBuilder().subject_name(name).issuer_name(name)
          .public_key(key.public_key()).serial_number(x509.random_serial_number())
          .not_valid_before(now-timedelta(hours=1)).not_valid_after(now+timedelta(hours=1))
          .add_extension(x509.BasicConstraints(ca=ca,path_length=None),critical=True))
    if usage:
        builder=builder.add_extension(x509.KeyUsage(digital_signature=digital,content_commitment=False,
             key_encipherment=False,data_encipherment=False,key_agreement=False,
             key_cert_sign=ca,crl_sign=ca,encipher_only=False,decipher_only=False),critical=True)
    cert=builder.sign(key,None if isinstance(key,ed25519.Ed25519PrivateKey) else hashes.SHA256())
    return cert.public_bytes(serialization.Encoding.PEM)


@pytest.mark.parametrize('mode',['valid','pin','expired','algorithm','usage','clock','authority','rotated'])
def test_certificate_inspector_live_boundaries(tmp_path,mode):
    now=datetime(2026,10,12,tzinfo=timezone.utc)
    chain=certificate(now,digital=mode!='usage')
    _,manifest=fixture(tmp_path)
    raw=manifest['policies'][0]
    raw['certificate_chain_sha']=sha256(chain).hexdigest()
    policy=SigningPolicy.model_validate(raw)
    calls=[]
    times=iter([now,now+timedelta(hours=2)]) if mode=='rotated' else None
    def clock():
        if times is not None:return next(times)
        if mode=='clock':return now.replace(tzinfo=None)
        if mode=='expired':return now+timedelta(hours=2)
        return now
    def live(*args):
        calls.append(args)
        return False if mode=='authority' else True
    inspector=CertificateBoundInstallationInspector(live,clock=clock)
    if mode=='pin':chain+=b'\n'
    if mode in ('pin','expired','algorithm','usage','clock','rotated'):
        with pytest.raises(ValueError):inspector(policy,'ES256' if mode=='algorithm' else 'ED25519',chain)
        assert len(calls)==(1 if mode=='rotated' else 0)
    else:
        assert inspector(policy,'ED25519',chain) is (mode=='valid')
        assert len(calls)==1


@pytest.mark.parametrize('kind,algorithm,allowed',[
    ('rsa','PS256',True),('rsa','PS384',True),('rsa','PS512',True),
    ('weak-rsa','PS256',False),('p256','ES256',True),('p384','ES384',True),
    ('p521','ES512',True),('p256','ES384',False),('p384','ES256',False),
    ('ed25519','ED25519',True),('ed25519','PS256',False),('rsa','ES256',False),
    ('ed25519','unknown',False)])
def test_certificate_inspector_algorithm_matrix(tmp_path,kind,algorithm,allowed):
    now=datetime(2026,10,12,tzinfo=timezone.utc)
    if kind in ('rsa','weak-rsa'):
        key=rsa.generate_private_key(public_exponent=65537,key_size=1024 if kind=='weak-rsa' else 2048)
    elif kind.startswith('p'):
        key=ec.generate_private_key({'p256':ec.SECP256R1,'p384':ec.SECP384R1,'p521':ec.SECP521R1}[kind]())
    else:key=ed25519.Ed25519PrivateKey.generate()
    chain=certificate(now,key=key)
    _,manifest=fixture(tmp_path);raw=manifest['policies'][0]
    raw['certificate_chain_sha']=sha256(chain).hexdigest();policy=SigningPolicy.model_validate(raw)
    calls=[]
    inspector=CertificateBoundInstallationInspector(lambda *args:calls.append(args) or True,clock=lambda:now)
    if allowed:assert inspector(policy,algorithm,chain) is True
    else:
        with pytest.raises(ValueError):inspector(policy,algorithm,chain)
    assert len(calls)==int(allowed)


@pytest.mark.parametrize('mode',['ca','missing-usage','mutated-policy','cancelled'])
def test_certificate_inspector_refuses_invalid_leaf_or_callback(tmp_path,mode):
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    now=datetime(2026,10,12,tzinfo=timezone.utc)
    chain=certificate(now,ca=mode=='ca',usage=mode!='missing-usage')
    _,manifest=fixture(tmp_path);raw=manifest['policies'][0]
    raw['certificate_chain_sha']=sha256(chain).hexdigest();policy=SigningPolicy.model_validate(raw)
    active=True;calls=[]
    def guard():
        if not active:raise RuntimeError('cancelled')
    def live(owned,*args):
        nonlocal active
        calls.append(owned)
        if mode=='mutated-policy':owned.allowed_mime.append('image/jpeg')
        if mode=='cancelled':active=False
        return True
    inspector=CertificateBoundInstallationInspector(live,clock=lambda:now)
    with signing_task_scope(guard):
        with pytest.raises(RuntimeError if mode=='cancelled' else ValueError):
            inspector(policy,'ED25519',chain)
        active=True
    assert len(calls)==int(mode in ('mutated-policy','cancelled'))
    assert policy.allowed_mime==['image/png']


def test_certificate_inspector_rejects_non_ca_chain_issuer(tmp_path):
    now = datetime(2026, 10, 12, tzinfo=timezone.utc)
    chain = certificate(now) + certificate(now, ca=False)
    _, manifest = fixture(tmp_path)
    raw = manifest['policies'][0]
    raw['certificate_chain_sha'] = sha256(chain).hexdigest()
    policy = SigningPolicy.model_validate(raw)
    with pytest.raises(ValueError, match='ISSUER_USAGE'):
        CertificateBoundInstallationInspector(lambda *args:pytest.fail('live authority called'),
            clock=lambda:now)(policy, 'ED25519', chain)


@pytest.mark.parametrize('issuer_matches', [True, False])
def test_certificate_inspector_verifies_intermediate_signature_before_live_authority(tmp_path, issuer_matches):
    now = datetime(2026, 10, 12, tzinfo=timezone.utc)
    issuer_key = ed25519.Ed25519PrivateKey.generate()
    wrong_key = ed25519.Ed25519PrivateKey.generate()
    leaf_key = ed25519.Ed25519PrivateKey.generate()
    issuer_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'pinned issuer')])
    leaf_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'signer leaf')])
    issuer = (x509.CertificateBuilder().subject_name(issuer_name).issuer_name(issuer_name)
        .public_key(issuer_key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now-timedelta(hours=1)).not_valid_after(now+timedelta(hours=1))
        .add_extension(x509.BasicConstraints(ca=True,path_length=None),critical=True)
        .add_extension(x509.KeyUsage(digital_signature=True,content_commitment=False,
            key_encipherment=False,data_encipherment=False,key_agreement=False,
            key_cert_sign=True,crl_sign=True,encipher_only=False,decipher_only=False),critical=True)
        .sign(issuer_key,None))
    leaf = (x509.CertificateBuilder().subject_name(leaf_name).issuer_name(issuer_name)
        .public_key(leaf_key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now-timedelta(hours=1)).not_valid_after(now+timedelta(hours=1))
        .add_extension(x509.BasicConstraints(ca=False,path_length=None),critical=True)
        .add_extension(x509.KeyUsage(digital_signature=True,content_commitment=False,
            key_encipherment=False,data_encipherment=False,key_agreement=False,
            key_cert_sign=False,crl_sign=False,encipher_only=False,decipher_only=False),critical=True)
        .sign(issuer_key if issuer_matches else wrong_key,None))
    chain = (leaf.public_bytes(serialization.Encoding.PEM)
             + issuer.public_bytes(serialization.Encoding.PEM))
    _, manifest = fixture(tmp_path)
    raw = manifest['policies'][0]
    raw['certificate_chain_sha'] = sha256(chain).hexdigest()
    trust = issuer.public_bytes(serialization.Encoding.PEM)
    raw['trust_bundle_sha'] = sha256(trust).hexdigest()
    policy = SigningPolicy.model_validate(raw)
    called = []
    inspector = CertificateBoundInstallationInspector(
        lambda *args: called.append(True) or True, trust_bundle=trust,
        clock=lambda: now)
    if issuer_matches:
        assert inspector(policy, 'ED25519', chain) is True
        assert called == [True]
    else:
        with pytest.raises(ValueError, match='SIGNER_CERTIFICATE_CHAIN_SIGNATURE'):
            inspector(policy, 'ED25519', chain)
        assert called == []


def test_certificate_inspector_binds_deployed_trust_bundle_before_live_authority(tmp_path):
    now = datetime(2026, 10, 12, tzinfo=timezone.utc)
    chain = certificate(now)
    wrong_root = certificate(now)
    _, manifest = fixture(tmp_path)
    raw = manifest['policies'][0]
    raw['certificate_chain_sha'] = sha256(chain).hexdigest()
    raw['trust_bundle_sha'] = sha256(wrong_root).hexdigest()
    policy = SigningPolicy.model_validate(raw)
    called = []
    wrong = CertificateBoundInstallationInspector(
        lambda *args: called.append(True) or True,
        trust_bundle=wrong_root, clock=lambda: now)
    with pytest.raises(ValueError, match='SIGNER_TRUST_ANCHOR'):
        wrong(policy, 'ED25519', chain)
    assert called == []
    raw['trust_bundle_sha'] = sha256(chain).hexdigest()
    policy = SigningPolicy.model_validate(raw)
    pinned = CertificateBoundInstallationInspector(
        lambda *args: called.append(True) or True,
        trust_bundle=chain, clock=lambda: now)
    assert pinned(policy, 'ED25519', chain) is True
    assert called == [True]


@pytest.mark.parametrize('root_path_length, accepted', [(0, False), (1, True)])
def test_trust_anchor_enforces_intermediate_ca_path_length(tmp_path, root_path_length, accepted):
    now = datetime(2026, 10, 12, tzinfo=timezone.utc)
    root_key = ed25519.Ed25519PrivateKey.generate()
    intermediate_key = ed25519.Ed25519PrivateKey.generate()
    leaf_key = ed25519.Ed25519PrivateKey.generate()
    root_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'root')])
    intermediate_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'intermediate')])
    leaf_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'leaf')])
    def issue(subject, issuer, public, signer, ca, path_length):
        return (x509.CertificateBuilder().subject_name(subject).issuer_name(issuer)
            .public_key(public).serial_number(x509.random_serial_number())
            .not_valid_before(now-timedelta(hours=1)).not_valid_after(now+timedelta(hours=1))
            .add_extension(x509.BasicConstraints(ca=ca,path_length=path_length),critical=True)
            .add_extension(x509.KeyUsage(digital_signature=True,content_commitment=False,
                key_encipherment=False,data_encipherment=False,key_agreement=False,
                key_cert_sign=ca,crl_sign=ca,encipher_only=False,decipher_only=False),critical=True)
            .sign(signer,None).public_bytes(serialization.Encoding.PEM))
    root = issue(root_name, root_name, root_key.public_key(), root_key, True, root_path_length)
    intermediate = issue(intermediate_name, root_name, intermediate_key.public_key(),
                         root_key, True, 0)
    leaf = issue(leaf_name, intermediate_name, leaf_key.public_key(),
                 intermediate_key, False, None)
    chain = leaf + intermediate
    _, manifest = fixture(tmp_path)
    raw = manifest['policies'][0]
    raw['certificate_chain_sha'] = sha256(chain).hexdigest()
    raw['trust_bundle_sha'] = sha256(root).hexdigest()
    policy = SigningPolicy.model_validate(raw)
    called = []
    inspector = CertificateBoundInstallationInspector(
        lambda *args: called.append(True) or True, trust_bundle=root,
        clock=lambda: now)
    if accepted:
        assert inspector(policy, 'ED25519', chain) is True
        assert called == [True]
    else:
        with pytest.raises(ValueError, match='SIGNER_TRUST_ANCHOR'):
            inspector(policy, 'ED25519', chain)
        assert called == []
