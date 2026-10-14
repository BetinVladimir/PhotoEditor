"""Native C2PA delegate with external-key callbacks; no private key material.

Provisioning owns SDK identity, live key/certificate checks and independent
embedded-manifest/certificate verification. These are mandatory dependencies,
not worker-generated attestations. API: contentauth/c2pa-python Signer.from_callback.
"""
from contextlib import contextmanager
from hashlib import sha256
from urllib.parse import urlsplit

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy
from .native_context import PinnedSigningContext
from .binary_binding import verify_binary_binding
from .cancellation import check_signing_active,bind_current_signing_task


class NativeSigningDelegate:
    def __init__(self, sdk, sdk_lock_sha, trust_bundle, certificate_chain,
                 algorithm, sign_payload, inspect_installation, verify_binding,
                 timestamp_services=None, network_hosts=()):
        if (not isinstance(certificate_chain, bytes)
                or not 1 <= len(certificate_chain) <= 1 << 20
                or algorithm not in ('ES256', 'ES384', 'ES512', 'PS256', 'PS384', 'PS512', 'ED25519')
                or not all(callable(fn) for fn in (sign_payload, inspect_installation, verify_binding))):
            raise ValueError('SIGNER_NATIVE_INSTALLATION')
        try:
            certs = certificate_chain.decode('ascii')
        except UnicodeDecodeError as exc:
            raise ValueError('SIGNER_NATIVE_CERTIFICATE') from exc
        if ('-----BEGIN CERTIFICATE-----' not in certs
                or '-----END CERTIFICATE-----' not in certs or 'PRIVATE KEY' in certs):
            raise ValueError('SIGNER_NATIVE_CERTIFICATE')
        self._context = PinnedSigningContext(sdk, sdk_lock_sha, trust_bundle, network_hosts)
        services = {} if timestamp_services is None else dict(timestamp_services)
        if len(services) > 64:
            raise ValueError('SIGNER_NATIVE_TIMESTAMP')
        for name, url in services.items():
            if (not isinstance(name, str) or not name or len(name.encode()) > 120
                    or name.strip() != name or any(c in name for c in '\x00\r\n')
                    or not isinstance(url, str) or len(url) > 2048
                    or any(c.isspace() or ord(c) < 32 for c in url)):
                raise ValueError('SIGNER_NATIVE_TIMESTAMP')
            parsed = urlsplit(url)
            if (parsed.scheme != 'https' or not parsed.hostname
                    or parsed.username is not None or parsed.password is not None
                    or parsed.fragment or parsed.hostname not in network_hosts
                    or parsed.port not in (None, 443)):
                raise ValueError('SIGNER_NATIVE_TIMESTAMP')
        self._sdk, self._sdk_sha = sdk, sdk_lock_sha
        self._certificate, self._certs = bytes(certificate_chain), certs
        self._certificate_sha = sha256(certificate_chain).hexdigest()
        self._trust_sha = sha256(trust_bundle).hexdigest()
        self._algorithm = algorithm
        self._sign, self._inspect, self._verify = sign_payload, inspect_installation, verify_binding
        self._services = services

    def check_installation(self, policy):
        check_signing_active()
        if (not isinstance(policy, SigningPolicy) or policy.sdk_lock_sha != self._sdk_sha
                or policy.certificate_chain_sha != self._certificate_sha
                or policy.trust_bundle_sha != self._trust_sha
                or (policy.timestamp_mode == 'required' and policy.timestamp_service_id not in self._services)):
            raise ValueError('SIGNER_NATIVE_POLICY')
        # Inspector must check actual key ownership, certificate algorithm,
        # expiry/revocation, native binary lock and deployed network restrictions.
        if self._inspect(policy.model_copy(deep=True), self._algorithm, self._certificate) is not True:
            raise ValueError('SIGNER_NATIVE_UNQUALIFIED')
        check_signing_active()

    def sign_operator_challenge(self, policy, key_handle, algorithm, payload):
        """Possession proof only, for an authenticated process-owned endpoint.

        This method is not an HTTP route and must never be exposed to task or
        public API payloads without the separate authenticated transport.
        """
        import re
        from photo_worker.contracts.base import parse_json
        check_signing_active()
        if (not isinstance(policy, SigningPolicy) or key_handle != policy.key_handle
                or not isinstance(payload, bytes) or not 2 <= len(payload) <= 4096
                or not (algorithm == self._algorithm
                        or algorithm == 'PS256' and self._algorithm in ('PS384', 'PS512'))):
            raise ValueError('SIGNER_OPERATOR_CHALLENGE')
        owned = policy.model_copy(deep=True)
        body = parse_json(payload, max_bytes=4096)
        if (not isinstance(body, dict) or set(body) != {'domain', 'policy_sha256', 'nonce'}
                or canonical_bytes(body) != payload
                or body['domain'] != 'photoeditor.signing-operator-possession/v1'
                or body['policy_sha256'] != sha256(canonical_bytes(owned)).hexdigest()
                or not isinstance(body['nonce'], str)
                or re.fullmatch(r'[0-9a-f]{64}', body['nonce']) is None):
            raise ValueError('SIGNER_OPERATOR_CHALLENGE')
        self.check_installation(owned)
        signature = self._sign(key_handle, algorithm, bytes(payload))
        if not isinstance(signature, bytes) or not 1 <= len(signature) <= 1024:
            raise ValueError('SIGNER_OPERATOR_CHALLENGE_SIGNATURE')
        self.check_installation(owned)
        check_signing_active()
        return signature

    @contextmanager
    def open_context(self, policy):
        owned = policy.model_copy(deep=True)
        self.check_installation(owned)
        with self._context.open_context(owned) as context:
            yield context
        self.check_installation(owned)

    @contextmanager
    def open_signer(self, policy):
        owned = policy.model_copy(deep=True)
        self.check_installation(owned)
        # Callback lifetime is exactly the native handle lifetime. A stale
        # callback cannot keep using a key after the context manager closes.
        active = True
        @bind_current_signing_task
        def callback(payload):
            if not active or not isinstance(payload, bytes) or not 1 <= len(payload) <= 1 << 20:
                raise ValueError('SIGNER_NATIVE_PAYLOAD')
            self.check_installation(owned)
            signature = self._sign(owned.key_handle, self._algorithm, bytes(payload))
            if not active:
                raise ValueError('SIGNER_NATIVE_PAYLOAD')
            if not isinstance(signature, bytes) or not 1 <= len(signature) <= 16384:
                raise ValueError('SIGNER_NATIVE_SIGNATURE')
            self.check_installation(owned)
            if not active:
                raise ValueError('SIGNER_NATIVE_PAYLOAD')
            return signature
        tsa = self._services[owned.timestamp_service_id] if owned.timestamp_mode == 'required' else None
        try:
            with self._sdk.Signer.from_callback(callback,
                    getattr(self._sdk.C2paSigningAlg, self._algorithm), self._certs, tsa) as signer:
                self.check_installation(owned)
                yield signer
        finally:
            active = False
        self.check_installation(owned)

    def verify_reader(self, reader, policy, lineage, manifest):
        owned = policy.model_copy(deep=True)
        self.check_installation(owned)
        if not isinstance(manifest, bytes) or not 1 <= len(manifest) <= 8 << 20:
            raise ValueError('SIGNER_NATIVE_MANIFEST')
        # A Trusted status alone does not prove the expected certificate or
        # exact embedded binary store. The independent verifier must prove both.
        before = canonical_bytes(lineage)
        if self._verify(reader, owned.model_copy(deep=True), lineage.model_copy(deep=True),
                        bytes(manifest), self._certificate) is not True:
            raise ValueError('SIGNER_NATIVE_BINDING')
        verify_binary_binding(reader, owned, lineage, manifest, self._certificate, self._algorithm,
                              check_active=check_signing_active)
        if canonical_bytes(lineage) != before:
            raise ValueError('SIGNER_NATIVE_LINEAGE_CHANGED')
        self.check_installation(owned)
