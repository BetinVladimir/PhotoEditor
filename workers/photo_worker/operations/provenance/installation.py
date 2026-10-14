"""Owned operator policy installation, supplied by trusted process construction.

This binding does not qualify an SDK/certificate implementation. The supplied
authority must verify live native installation, key, trust and network policy;
the delegate must independently verify the actual C2PA certificate chain.
Neither dependency is selected from task JSON or environment import strings.
"""
from contextlib import contextmanager
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy, decode_provenance


class InstalledSigningOperator:
    def __init__(self, policies, authority, delegate):
        if not isinstance(policies, (list, tuple)) or not 1 <= len(policies) <= 256:
            raise ValueError('SIGNER_INSTALLATION_POLICIES')
        if any(not isinstance(raw, bytes) for raw in policies) or sum(len(raw) for raw in policies) > 8 << 20:
            raise ValueError('SIGNER_INSTALLATION_BYTES')
        if not callable(authority) or delegate is None:
            raise ValueError('SIGNER_INSTALLATION_AUTHORITY')
        self._policies = {}
        for raw in policies:
            policy = decode_provenance(raw, SigningPolicy, 1 << 20)
            key = (policy.id, policy.revision)
            if key in self._policies:
                raise ValueError('SIGNER_INSTALLATION_DUPLICATE')
            self._policies[key] = bytes(raw)
        self._authority, self._delegate = authority, delegate

    def policy_pins(self):
        """Expose only exact installed policy identities for scheduler readiness."""
        return tuple({'id': str(policy_id), 'sha': sha256(raw).hexdigest()}
                     for (policy_id, _revision), raw in sorted(
                         self._policies.items(), key=lambda entry: (str(entry[0][0]), entry[0][1])))

    def check_policy_pins(self):
        """Requalify every advertised policy against live native trust."""
        for raw in tuple(self._policies.values()):
            self.check_installation(decode_provenance(raw, SigningPolicy, 1 << 20))
        return True

    def _owned(self, policy):
        if not isinstance(policy, SigningPolicy):
            raise ValueError('SIGNER_INSTALLATION_POLICY')
        raw = canonical_bytes(policy)
        installed = self._policies.get((policy.id, policy.revision))
        if installed is None or raw != installed:
            raise ValueError('SIGNER_POLICY_NOT_INSTALLED')
        return decode_provenance(installed, SigningPolicy, 1 << 20), sha256(installed).hexdigest()

    def check_installation(self, policy):
        owned, digest = self._owned(policy)
        # A strict bool is intentional: truthy objects are not attestations.
        if self._authority(owned.model_copy(deep=True), digest) is not True:
            raise ValueError('SIGNER_INSTALLATION_UNQUALIFIED')
        self._delegate.check_installation(owned.model_copy(deep=True))
        # Detect mutation by either dependency before returning to the bridge.
        self._owned(owned)
        if self._authority(owned.model_copy(deep=True), digest) is not True:
            raise ValueError('SIGNER_INSTALLATION_REVOKED')

    def sign_operator_challenge(self, policy, key_handle, algorithm, payload):
        owned, digest = self._owned(policy)
        self.check_installation(owned)
        signature = self._delegate.sign_operator_challenge(
            owned.model_copy(deep=True), key_handle, algorithm, payload)
        if not isinstance(signature, bytes) or not 1 <= len(signature) <= 1024:
            raise ValueError('SIGNER_OPERATOR_CHALLENGE_SIGNATURE')
        self.check_installation(owned)
        return signature

    @contextmanager
    def open_context(self, policy):
        self.check_installation(policy)
        owned, _ = self._owned(policy)
        with self._delegate.open_context(owned) as context:
            self._owned(owned)
            yield context
        self._owned(owned)
        self.check_installation(policy)

    @contextmanager
    def open_signer(self, policy):
        self.check_installation(policy)
        owned, _ = self._owned(policy)
        with self._delegate.open_signer(owned) as signer:
            self._owned(owned)
            yield signer
        self._owned(owned)
        self.check_installation(policy)

    def verify_reader(self, reader, policy, lineage, manifest):
        self.check_installation(policy)
        owned, _ = self._owned(policy)
        self._delegate.verify_reader(reader, owned, lineage.model_copy(deep=True), bytes(manifest))
        self._owned(owned)
        self.check_installation(policy)
