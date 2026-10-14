"""Certificate checks composed with mandatory independent live authority.

This validates pinned certificate bytes and signing capability. It does not
establish chain trust, revocation, SDK identity or network controls by itself.
"""
from datetime import datetime, timezone
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy
from .cancellation import check_signing_active


def _subordinate_ca_count(certificates):
    from cryptography import x509
    count = 0
    for certificate in certificates:
        try:
            is_ca = certificate.extensions.get_extension_for_class(x509.BasicConstraints).value.ca
        except x509.ExtensionNotFound:
            is_ca = False
        if is_ca and certificate.subject != certificate.issuer:
            count += 1
    return count


def check_certificate_structure(policy, algorithm, certificate_chain, now):
    """Check pinned certificate bytes, key use and validity; no chain trust claim."""
    if not isinstance(now, datetime) or now.tzinfo is None or now.utcoffset() is None:
        raise ValueError('SIGNER_CERTIFICATE_CLOCK')
    check_signing_active()
    if (not isinstance(policy, SigningPolicy)
            or not isinstance(certificate_chain, bytes)
            or not 1 <= len(certificate_chain) <= 1 << 20
            or sha256(certificate_chain).hexdigest() != policy.certificate_chain_sha):
        raise ValueError('SIGNER_CERTIFICATE_PIN')
    from cryptography import x509
    from cryptography.exceptions import InvalidSignature, UnsupportedAlgorithm
    from cryptography.hazmat.primitives.asymmetric import rsa, ec, ed25519
    certificates = x509.load_pem_x509_certificates(certificate_chain)
    if not 1 <= len(certificates) <= 16:
        raise ValueError('SIGNER_CERTIFICATE_CHAIN')
    leaf = certificates[0]
    for index, (child, issuer) in enumerate(zip(certificates, certificates[1:])):
        check_signing_active()
        if child.issuer != issuer.subject:
            raise ValueError('SIGNER_CERTIFICATE_CHAIN_ORDER')
        try:
            constraints = issuer.extensions.get_extension_for_class(x509.BasicConstraints).value
            issuer_usage = issuer.extensions.get_extension_for_class(x509.KeyUsage).value
        except x509.ExtensionNotFound:
            raise ValueError('SIGNER_CERTIFICATE_ISSUER_USAGE') from None
        if not constraints.ca or not issuer_usage.key_cert_sign:
            raise ValueError('SIGNER_CERTIFICATE_ISSUER_USAGE')
        if (constraints.path_length is not None
                and _subordinate_ca_count(certificates[1:index+1]) > constraints.path_length):
            raise ValueError('SIGNER_CERTIFICATE_PATH_LENGTH')
        # Matching issuer/subject names alone do not prove the certificate was
        # issued by this key. Trust and revocation remain external authorities.
        try:
            child.verify_directly_issued_by(issuer)
        except (ValueError, InvalidSignature, UnsupportedAlgorithm) as exc:
            raise ValueError('SIGNER_CERTIFICATE_CHAIN_SIGNATURE') from exc
    try:
        if leaf.extensions.get_extension_for_class(x509.BasicConstraints).value.ca:
            raise ValueError('SIGNER_CERTIFICATE_LEAF_CA')
    except x509.ExtensionNotFound:
        pass
    key = leaf.public_key()
    curves = {'ES256': ec.SECP256R1, 'ES384': ec.SECP384R1, 'ES512': ec.SECP521R1}
    if algorithm in ('PS256', 'PS384', 'PS512'):
        valid = isinstance(key, rsa.RSAPublicKey) and 2048 <= key.key_size <= 8192
    elif algorithm in curves:
        valid = isinstance(key, ec.EllipticCurvePublicKey) and isinstance(key.curve, curves[algorithm])
    elif algorithm == 'ED25519':
        valid = isinstance(key, ed25519.Ed25519PublicKey)
    else:
        valid = False
    if not valid:
        raise ValueError('SIGNER_CERTIFICATE_ALGORITHM')
    try:
        usage = leaf.extensions.get_extension_for_class(x509.KeyUsage).value
    except x509.ExtensionNotFound:
        raise ValueError('SIGNER_CERTIFICATE_KEY_USAGE') from None
    if not usage.digital_signature:
        raise ValueError('SIGNER_CERTIFICATE_KEY_USAGE')
    if any(not (item.not_valid_before_utc <= now < item.not_valid_after_utc) for item in certificates):
        raise ValueError('SIGNER_CERTIFICATE_EXPIRED')
    check_signing_active()
    return True


def check_trust_anchor(policy, certificate_chain, trust_bundle, now):
    """Bind the verified chain to a pinned root or explicitly pinned leaf."""
    from cryptography import x509
    from cryptography.exceptions import InvalidSignature, UnsupportedAlgorithm
    from cryptography.hazmat.primitives import serialization
    if (not isinstance(policy, SigningPolicy) or not isinstance(trust_bundle, bytes)
            or not 1 <= len(trust_bundle) <= 1 << 20
            or sha256(trust_bundle).hexdigest() != policy.trust_bundle_sha):
        raise ValueError('SIGNER_TRUST_BUNDLE_PIN')
    check_signing_active()
    chain = x509.load_pem_x509_certificates(certificate_chain)
    anchors = x509.load_pem_x509_certificates(trust_bundle)
    if not 1 <= len(anchors) <= 64:
        raise ValueError('SIGNER_TRUST_BUNDLE')
    top = chain[-1]
    top_der = top.public_bytes(serialization.Encoding.DER)
    for anchor in anchors:
        check_signing_active()
        if not anchor.not_valid_before_utc <= now < anchor.not_valid_after_utc:
            continue
        if anchor.public_bytes(serialization.Encoding.DER) == top_der:
            return True
        try:
            constraints = anchor.extensions.get_extension_for_class(x509.BasicConstraints).value
            usage = anchor.extensions.get_extension_for_class(x509.KeyUsage).value
            if constraints.ca and usage.key_cert_sign:
                if (constraints.path_length is not None
                        and _subordinate_ca_count(chain[1:]) > constraints.path_length):
                    continue
                top.verify_directly_issued_by(anchor)
                return True
        except (x509.ExtensionNotFound, ValueError, InvalidSignature, UnsupportedAlgorithm):
            continue
    raise ValueError('SIGNER_TRUST_ANCHOR')


class CertificateBoundInstallationInspector:
    def __init__(self, live_inspector, *, trust_bundle=None,
                 clock=lambda: datetime.now(timezone.utc)):
        if not callable(live_inspector) or not callable(clock):
            raise ValueError('SIGNER_CERTIFICATE_INSPECTOR')
        if trust_bundle is not None and not isinstance(trust_bundle, bytes):
            raise ValueError('SIGNER_TRUST_BUNDLE')
        self._live, self._clock = live_inspector, clock
        self._trust = trust_bundle

    def __call__(self, policy, algorithm, certificate_chain):
        if not isinstance(policy, SigningPolicy):
            raise ValueError('SIGNER_CERTIFICATE_PIN')
        owned = policy.model_copy(deep=True)
        before = canonical_bytes(owned)
        now = self._clock()
        check_certificate_structure(owned, algorithm, certificate_chain, now)
        if self._trust is not None:
            check_trust_anchor(owned, certificate_chain, self._trust, now)
        accepted = self._live(owned, algorithm, certificate_chain)
        now = self._clock()
        check_certificate_structure(owned, algorithm, certificate_chain, now)
        if self._trust is not None:
            check_trust_anchor(owned, certificate_chain, self._trust, now)
        if canonical_bytes(policy) != before or canonical_bytes(owned) != before:
            raise ValueError('SIGNER_CERTIFICATE_POLICY_CHANGED')
        return accepted is True
