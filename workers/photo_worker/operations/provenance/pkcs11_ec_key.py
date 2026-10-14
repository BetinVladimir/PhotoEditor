"""ECDSA external-key adapter for python-pkcs11 and C2PA callbacks.

The loaded native module is supplied by qualified process construction. No task
selects a module, token, key ID or PIN. Actual trust/revocation and native-module
qualification remain independent of this key/certificate binding check.
"""
from datetime import datetime, timezone
from hashlib import sha256
from secrets import token_bytes
from threading import Lock
from .cancellation import check_signing_active


class PKCS11ECKey:
    def __init__(self, pkcs11, library, *, key_handle, token_serial, key_id,
                 certificate_chain, pin_supplier, check_active=lambda: None):
        from cryptography import x509
        from cryptography.hazmat.primitives.asymmetric import ec
        if (not isinstance(key_handle, str) or not 1 <= len(key_handle.encode()) <= 200
                or key_handle.strip() != key_handle or any(c in key_handle for c in '\x00\r\n')
                or not isinstance(token_serial, bytes) or not 1 <= len(token_serial) <= 64
                or not isinstance(key_id, bytes) or not 1 <= len(key_id) <= 128
                or not isinstance(certificate_chain, bytes) or not 1 <= len(certificate_chain) <= 1 << 20
                or not callable(pin_supplier) or not callable(check_active)):
            raise ValueError('SIGNER_PKCS11_INSTALLATION')
        certs = x509.load_pem_x509_certificates(certificate_chain)
        if not certs or len(certs) > 16:
            raise ValueError('SIGNER_PKCS11_CERTIFICATE')
        public = certs[0].public_key()
        if not isinstance(public, ec.EllipticCurvePublicKey) or public.curve.name not in ('secp256r1', 'secp384r1', 'secp521r1'):
            raise ValueError('SIGNER_PKCS11_EC_KEY')
        self._api, self._library = pkcs11, library
        self._handle, self._serial, self._id = key_handle, bytes(token_serial), bytes(key_id)
        self._certificates, self._public = tuple(certs), public
        self._certificate_sha = sha256(certificate_chain).hexdigest()
        self._pin, self._active = pin_supplier, check_active
        self._lock = Lock()

    def _validity(self):
        check_signing_active()
        self._active()
        now = datetime.now(timezone.utc)
        if any(not cert.not_valid_before_utc <= now < cert.not_valid_after_utc for cert in self._certificates):
            raise ValueError('SIGNER_PKCS11_CERTIFICATE_EXPIRED')

    def sign_payload(self, key_handle, algorithm, payload):
        from cryptography.hazmat.primitives import hashes
        from cryptography.hazmat.primitives.asymmetric import ec, utils
        if (key_handle != self._handle or algorithm not in ('ES256', 'ES384', 'ES512')
                or not isinstance(payload, bytes) or not 1 <= len(payload) <= 1 << 20):
            raise ValueError('SIGNER_PKCS11_REQUEST')
        expected_curve = {'ES256': 'secp256r1', 'ES384': 'secp384r1', 'ES512': 'secp521r1'}[algorithm]
        if self._public.curve.name != expected_curve:
            raise ValueError('SIGNER_PKCS11_EC_ALGORITHM')
        bits = algorithm[2:]
        digest = {'256': hashes.SHA256, '384': hashes.SHA384, '512': hashes.SHA512}[bits]()
        hasher = hashes.Hash(digest)
        hasher.update(payload)
        hashed = hasher.finalize()
        mechanism = self._api.Mechanism.ECDSA
        # Tokens may share login/session state. Serialize this installation;
        # never retain a PIN or a live key/session after one callback.
        with self._lock:
            self._validity()
            token = self._library.get_token(token_serial=self._serial)
            if token.serial != self._serial:
                raise ValueError('SIGNER_PKCS11_TOKEN')
            pin = self._pin()
            if not isinstance(pin, (str, bytes)) or not 1 <= len(pin) <= 1024:
                raise ValueError('SIGNER_PKCS11_PIN')
            try:
                with token.open(rw=False, user_pin=pin) as session:
                    key = session.get_key(object_class=self._api.ObjectClass.PRIVATE_KEY,
                                          key_type=self._api.KeyType.EC, id=self._id)
                    attr = self._api.Attribute
                    if (key[attr.SIGN] is not True or key[attr.SENSITIVE] is not True
                            or key[attr.EXTRACTABLE] is not False):
                        raise ValueError('SIGNER_PKCS11_KEY_ATTRIBUTES')
                    self._validity()
                    raw = key.sign(hashed, mechanism=mechanism)
                    width = (self._public.key_size + 7) // 8
                    if not isinstance(raw, bytes) or len(raw) != 2 * width:
                        raise ValueError('SIGNER_PKCS11_SIGNATURE')
                    # PKCS#11 returns fixed-width r||s. The C2PA Python callback
                    # expects DER; the native SDK converts DER into COSE form.
                    r, s = int.from_bytes(raw[:width], 'big'), int.from_bytes(raw[width:], 'big')
                    if not r or not s:
                        raise ValueError('SIGNER_PKCS11_SIGNATURE')
                    signature = utils.encode_dss_signature(r, s)
                    self._public.verify(signature, payload, ec.ECDSA(digest))
                    self._validity()
                    return signature
            finally:
                pin = None


    def check_available(self, key_handle, algorithm, certificate_chain):
        """Prove current private-key possession; not chain/native qualification.

        A fresh domain-separated challenge prevents an old health signature from
        attesting a removed/replaced token. sign_payload verifies the signature
        with the deployment leaf certificate and closes the token session.
        """
        if (not isinstance(certificate_chain, bytes)
                or not 1 <= len(certificate_chain) <= 1 << 20
                or sha256(certificate_chain).hexdigest() != self._certificate_sha):
            raise ValueError('SIGNER_PKCS11_CERTIFICATE_BINDING')
        self.sign_payload(key_handle, algorithm,
            b'PhotoEditor/V31/PKCS11/key-availability/v1\x00' + token_bytes(32))
        return True
