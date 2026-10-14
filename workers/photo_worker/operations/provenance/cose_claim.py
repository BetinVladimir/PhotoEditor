"""Independent C2PA COSE Sign1 signature/certificate identity verification.

Does not establish chain trust, timestamp trust, OCSP or image hard binding.
Those remain mandatory SDK/policy checks. Original protected/claim bytes are
used directly in Sig_structure (RFC9052), never reserialized from parsed maps.
"""
from dataclasses import dataclass
from hashlib import sha256,sha384,sha512
from hmac import compare_digest

from .cbor import CBORTag,decode_cbor,encode_cbor_bytes


@dataclass(frozen=True)
class VerifiedClaimSignature:
    algorithm: str
    leaf_der_sha256: str
    claim_sha256: str


def verify_claim_signature(claim, cose, certificate_chain, *, check_active=lambda: None):
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes,serialization
    from cryptography.hazmat.primitives.asymmetric import ec,ed25519,padding,rsa,utils
    if (not isinstance(claim,bytes) or not 1<=len(claim)<=8<<20
            or not isinstance(certificate_chain,bytes) or not 1<=len(certificate_chain)<=1<<20
            or not callable(check_active)):
        raise ValueError('SIGNER_COSE_INPUT')
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    envelope=decode_cbor(cose,allowed_tags=(18,),check_active=guard)
    if not isinstance(envelope,CBORTag) or envelope.number!=18:
        raise ValueError('SIGNER_COSE_TAG')
    fields=envelope.value
    if (not isinstance(fields,list) or len(fields)!=4 or not isinstance(fields[0],bytes)
            or not fields[0] or not isinstance(fields[1],dict) or fields[2] is not None
            or not isinstance(fields[3],bytes) or not 1<=len(fields[3])<=16384):
        raise ValueError('SIGNER_COSE_SIGN1')
    protected_raw,unprotected,_,signature=fields
    protected=decode_cbor(protected_raw,maximum=1<<20,check_active=guard)
    if not isinstance(protected,dict) or set(protected)&set(unprotected):
        raise ValueError('SIGNER_COSE_HEADERS')
    algorithm=protected.get(1)
    algorithms={-7:('ES256',hashes.SHA256,ec.SECP256R1,32),
        -35:('ES384',hashes.SHA384,ec.SECP384R1,48),
        -36:('ES512',hashes.SHA512,ec.SECP521R1,66),
        -37:('PS256',hashes.SHA256,None,None),
        -38:('PS384',hashes.SHA384,None,None),
        -39:('PS512',hashes.SHA512,None,None),
        -8:('ED25519',None,None,None)}
    if type(algorithm) is not int or algorithm not in algorithms:
        raise ValueError('SIGNER_COSE_ALGORITHM')
    # Unknown critical headers cannot be ignored. Only alg/x5chain/x5t are
    # semantically processed here; SDK acceptance does not waive this check.
    if 2 in unprotected:raise ValueError('SIGNER_COSE_CRITICAL')
    if 2 in protected:
        critical=protected[2]
        if (not isinstance(critical,list) or not critical
                or any(type(key) is not int or key not in (1,33,34) or key not in protected for key in critical)
                or len(set(critical))!=len(critical)):
            raise ValueError('SIGNER_COSE_CRITICAL')
    # RFC9360 allows unprotected x5chain only when leaf identity is protected
    # (here by x5t). Exact full DER chain must still equal deployment pins.
    if 34 in unprotected:
        raise ValueError('SIGNER_COSE_CERTIFICATE_THUMBPRINT_UNPROTECTED')
    if 33 not in protected and 34 not in protected:
        raise ValueError('SIGNER_COSE_CERTIFICATE_INTEGRITY')
    chain=protected.get(33,unprotected.get(33))
    chain=[chain] if isinstance(chain,bytes) else chain
    if (not isinstance(chain,list) or not 1<=len(chain)<=16
            or any(not isinstance(der,bytes) or not 1<=len(der)<=1<<20 for der in chain)):
        raise ValueError('SIGNER_COSE_CERTIFICATE_HEADER')
    certificates=x509.load_pem_x509_certificates(certificate_chain)
    expected=[cert.public_bytes(serialization.Encoding.DER) for cert in certificates]
    if not expected or len(expected)>16 or chain!=expected:
        raise ValueError('SIGNER_COSE_CERTIFICATE_BINDING')
    if 34 in protected:
        thumbprint=protected[34]
        if (not isinstance(thumbprint,list) or len(thumbprint)!=2
                or type(thumbprint[0]) not in (int,str) or not isinstance(thumbprint[1],bytes)):
            raise ValueError('SIGNER_COSE_CERTIFICATE_THUMBPRINT')
        hash_algorithms={-16:sha256,'SHA-256':sha256,-43:sha384,'SHA-384':sha384,
                         -44:sha512,'SHA-512':sha512}
        hash_function=hash_algorithms.get(thumbprint[0])
        if hash_function is None or not compare_digest(hash_function(chain[0]).digest(),thumbprint[1]):
            raise ValueError('SIGNER_COSE_CERTIFICATE_THUMBPRINT')
    public=certificates[0].public_key()
    name,hash_type,curve,width=algorithms[algorithm]
    # Array4: context='Signature1', body_protected=bstr, external_aad=empty,
    # payload=original external claim bytes (detached content).
    signed=(b'\x84\x6aSignature1'+encode_cbor_bytes(protected_raw)+b'\x40'+encode_cbor_bytes(claim))
    guard()
    if name.startswith('PS'):
        if not isinstance(public,rsa.RSAPublicKey) or not 2048<=public.key_size<=8192:
            raise ValueError('SIGNER_COSE_KEY_ALGORITHM')
        digest=hash_type()
        if len(signature)!=(public.key_size+7)//8:raise ValueError('SIGNER_COSE_SIGNATURE_SIZE')
        public.verify(signature,signed,padding.PSS(mgf=padding.MGF1(digest),salt_length=digest.digest_size),digest)
    elif name.startswith('ES'):
        if not isinstance(public,ec.EllipticCurvePublicKey) or not isinstance(public.curve,curve):
            raise ValueError('SIGNER_COSE_KEY_ALGORITHM')
        if len(signature)!=2*width:raise ValueError('SIGNER_COSE_SIGNATURE_SIZE')
        der=utils.encode_dss_signature(int.from_bytes(signature[:width],'big'),int.from_bytes(signature[width:],'big'))
        public.verify(der,signed,ec.ECDSA(hash_type()))
    else:
        if not isinstance(public,ed25519.Ed25519PublicKey) or len(signature)!=64:
            raise ValueError('SIGNER_COSE_KEY_ALGORITHM')
        public.verify(signature,signed)
    guard()
    return VerifiedClaimSignature(name,sha256(chain[0]).hexdigest(),sha256(claim).hexdigest())
