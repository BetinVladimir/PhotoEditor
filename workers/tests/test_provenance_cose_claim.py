"""Real cryptographic Sign1 fixtures; self-signed certs are not trust evidence."""
from datetime import datetime,timedelta,timezone
from hashlib import sha256

import pytest

from photo_worker.operations.provenance.cbor import encode_cbor_bytes
from photo_worker.operations.provenance.cose_claim import verify_claim_signature


def fixture(algorithm=-37, claim=None, thumbprint=None):
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes,serialization
    from cryptography.hazmat.primitives.asymmetric import rsa,ec,ed25519,padding,utils
    from cryptography.x509.oid import NameOID
    if algorithm in (-37,-38,-39):key=rsa.generate_private_key(public_exponent=65537,key_size=2048)
    elif algorithm in (-7,-35,-36):key=ec.generate_private_key({-7:ec.SECP256R1,-35:ec.SECP384R1,-36:ec.SECP521R1}[algorithm]())
    else:key=ed25519.Ed25519PrivateKey.generate()
    name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'fixture')])
    now=datetime.now(timezone.utc)
    cert=(x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key())
        .serial_number(1).not_valid_before(now-timedelta(days=1)).not_valid_after(now+timedelta(days=1))
        .sign(key,None if algorithm==-8 else hashes.SHA256()))
    der=cert.public_bytes(serialization.Encoding.DER)
    # protected {1: algorithm, 33: DER}; canonical negative-integer encoding.
    negative=-1-algorithm
    encoded_alg=bytes([0x20+negative]) if negative<24 else b'\x38'+bytes([negative])
    protected=b'\xa2\x01'+encoded_alg+b'\x18\x21'+encode_cbor_bytes(der)
    unprotected=b'\xa0'
    if thumbprint is not None:
        from hashlib import sha384,sha512
        thumb_hash={'sha256':sha256,'sha384':sha384,'sha512':sha512}.get(thumbprint,sha256)(der).digest()
        hash_id={'sha256':b'\x2f','sha384':b'\x38\x2a','sha512':b'\x38\x2b',
                 'text':b'\x67SHA-256','unsupported':b'\x2d'}.get(thumbprint,b'\x2f')
        if thumbprint=='wrong':thumb_hash=b'\0'*32
        protected=b'\xa2\x01'+encoded_alg+b'\x18\x22\x82'+hash_id+encode_cbor_bytes(thumb_hash)
        unprotected=b'\xa1\x18\x21'+encode_cbor_bytes(der)
        if thumbprint=='missing':protected=b'\xa1\x01'+encoded_alg
    claim=b'original-claim-bytes' if claim is None else claim
    signed=b'\x84\x6aSignature1'+encode_cbor_bytes(protected)+b'\x40'+encode_cbor_bytes(claim)
    digest={-37:hashes.SHA256,-38:hashes.SHA384,-39:hashes.SHA512,-7:hashes.SHA256,-35:hashes.SHA384,-36:hashes.SHA512}.get(algorithm)
    if algorithm in (-37,-38,-39):
        signature=key.sign(signed,padding.PSS(mgf=padding.MGF1(digest()),salt_length=digest().digest_size),digest())
    elif algorithm in (-7,-35,-36):
        r,s=utils.decode_dss_signature(key.sign(signed,ec.ECDSA(digest())))
        width={-7:32,-35:48,-36:66}[algorithm]
        signature=r.to_bytes(width,'big')+s.to_bytes(width,'big')
    else:signature=key.sign(signed)
    cose=b'\xd2\x84'+encode_cbor_bytes(protected)+unprotected+b'\xf6'+encode_cbor_bytes(signature)
    return claim,cose,cert.public_bytes(serialization.Encoding.PEM),der


@pytest.mark.parametrize('algorithm,name',[(-37,'PS256'),(-38,'PS384'),(-39,'PS512'),
    (-7,'ES256'),(-35,'ES384'),(-36,'ES512'),(-8,'ED25519')])
def test_actual_signature_and_exact_certificate(algorithm,name):
    claim,cose,pem,der=fixture(algorithm)
    verified=verify_claim_signature(claim,cose,pem)
    assert verified.algorithm==name
    assert verified.leaf_der_sha256==sha256(der).hexdigest()
    assert verified.claim_sha256==sha256(claim).hexdigest()


def test_boolean_denial_refuses_cose_before_signature_verification():
    claim,cose,pem,_=fixture()
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        verify_claim_signature(claim,cose,pem,check_active=lambda:False)


@pytest.mark.parametrize('mode',['claim','signature','certificate','untagged','attached','trailing'])
def test_tampering_and_wrong_identity_refused(mode):
    from cryptography.exceptions import InvalidSignature
    claim,cose,pem,_=fixture()
    if mode=='claim':claim+=b'x'
    if mode=='signature':cose=cose[:-1]+bytes([cose[-1]^1])
    if mode=='certificate':pem=fixture()[2]
    if mode=='untagged':cose=cose[1:]
    if mode=='attached':cose=cose.replace(b'\xa0\xf6',b'\xa0\x40',1)
    if mode=='trailing':cose+=b'\0'
    with pytest.raises((ValueError,InvalidSignature)):verify_claim_signature(claim,cose,pem)


@pytest.mark.parametrize('mode',['valid','uri','active','algorithm','policy','lineage','reader_changed','inactive'])
def test_binary_jumbf_cose_and_sdk_join(mode):
    import json
    from struct import pack
    from types import SimpleNamespace
    from uuid import uuid4
    from photo_worker.contracts.provenance import SigningPolicy,SigningLineage
    from photo_worker.operations.provenance.binary_binding import verify_binary_binding
    from photo_worker.operations.provenance.jumbf_claim import type_uuid
    def box(kind,payload):return pack('>I',len(payload)+8)+kind+payload
    def superbox(kind,label,children):
        return box(b'jumb',box(b'jumd',type_uuid(kind)+b'\x03'+label.encode()+b'\0')+b''.join(children))
    uri='self#jumbf=c2pa.signature' if mode!='uri' else 'self#jumbf=/c2pa/other/c2pa.signature'
    encoded_uri=uri.encode()
    # CBOR map1: signature -> text URI; lengths use preferred encoding.
    text=(bytes([0x60+len(encoded_uri)]) if len(encoded_uri)<24 else b'\x78'+bytes([len(encoded_uri)]))+encoded_uri
    from photo_worker.contracts.base import canonical_bytes
    lineage=SigningLineage(schema_version=1,source_hashes=['a'*64],actions=[])
    assertion=superbox(b'json','org.photoeditor.lineage',[box(b'json',canonical_bytes(lineage))])
    # Hash includes assertion description/content, excludes outer jumb header.
    uri_bytes=b'self#jumbf=c2pa.assertions/org.photoeditor.lineage'
    reference=(b'\xa2\x63url\x78'+bytes([len(uri_bytes)])+uri_bytes
        +b'\x64hash'+encode_cbor_bytes(sha256(assertion[8:]).digest()))
    claim=(b'\xa5\x63alg\x66sha256\x69signature'+text
        +b'\x6ainstanceID\x66test:1'
        +b'\x72created_assertions\x81'+reference
        +b'\x74claim_generator_info\xa1\x64name\x6bPhotoEditor')
    claim,cose,pem,_=fixture(claim=claim)
    store=superbox(b'c2pa','c2pa',[superbox(b'c2ma','active',[
        superbox(b'c2cl','c2pa.claim.v2',[box(b'cbor',claim)]),
        superbox(b'c2as','c2pa.assertions',[assertion]),
        superbox(b'c2cs','c2pa.signature',[box(b'cbor',cose)])])])
    policy=SigningPolicy.model_validate(dict(schema_version=1,id=str(uuid4()),revision='1',
        sdk_lock_sha='a'*64,key_handle='key',certificate_chain_sha='b'*64 if mode=='policy' else sha256(pem).hexdigest(),
        timestamp_mode='off',timestamp_service_id=None,allowed_mime=['image/png'],trust_bundle_sha='c'*64,revocation_mode='off'))
    lineage=SigningLineage(schema_version=1,source_hashes=['a'*64],actions=[])
    active={'format':'image/png','assertions':[{'label':'org.photoeditor.lineage',
        'data':{} if mode=='lineage' else lineage.model_dump(mode='json')}]}
    document={'active_manifest':'other' if mode=='active' else 'active','manifests':{'active':active}}
    reads=0
    def reader_json():
        nonlocal reads
        reads+=1
        value=dict(document)
        if mode=='reader_changed' and reads>1:value['active_manifest']='other'
        return json.dumps(value)
    reader=SimpleNamespace(json=reader_json,get_active_manifest=lambda:active,
        is_embedded=lambda:True,get_validation_state=lambda:'Trusted',
        get_validation_results=lambda:{'activeManifest':{'failure':[],'informational':[],
            'success':[{'code':code} for code in ('claimSignature.validated',
            'claimSignature.insideValidity','signingCredential.trusted','assertion.dataHash.match')]}})
    if mode=='valid':
        assert verify_binary_binding(reader,policy,lineage,store,pem,'PS256').algorithm=='PS256'
    else:
        with pytest.raises((ValueError,RuntimeError)):
            verify_binary_binding(reader,policy,lineage,store,pem,
                'PS384' if mode=='algorithm' else 'PS256',
                check_active=lambda:False if mode=='inactive' else None)
        if mode=='inactive':assert reads==0


@pytest.mark.parametrize('thumbprint',['sha256','sha384','sha512','text'])
def test_unprotected_chain_requires_correct_protected_thumbprint(thumbprint):
    claim,cose,pem,_=fixture(thumbprint=thumbprint)
    assert verify_claim_signature(claim,cose,pem).algorithm=='PS256'


@pytest.mark.parametrize('thumbprint',['wrong','missing','unsupported'])
def test_valid_signature_does_not_override_wrong_or_missing_certificate_thumbprint(thumbprint):
    claim,cose,pem,_=fixture(thumbprint=thumbprint)
    with pytest.raises(ValueError,match='SIGNER_COSE_CERTIFICATE_'):
        verify_claim_signature(claim,cose,pem)
