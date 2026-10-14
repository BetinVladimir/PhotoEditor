"""Combine live key possession with mandatory independent native authority."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy


class KeyBoundInstallationInspector:
    def __init__(self, key_provider, native_inspector):
        if (not callable(getattr(key_provider, 'check_available', None))
                or not callable(native_inspector)):
            raise ValueError('SIGNER_KEY_INSPECTOR_INSTALLATION')
        self._key, self._native = key_provider, native_inspector

    def __call__(self, policy, algorithm, certificate_chain):
        if not isinstance(policy, SigningPolicy) or not isinstance(certificate_chain, bytes):
            raise ValueError('SIGNER_KEY_INSPECTOR_POLICY')
        # Freeze the policy before any adapter I/O. Each adapter receives its
        # own copy; neither can retarget the following authority/key check.
        owned = policy.model_copy(deep=True)
        before = canonical_bytes(owned)
        if self._native(owned.model_copy(deep=True), algorithm, bytes(certificate_chain)) is not True:
            return False
        if self._key.check_available(owned.key_handle, algorithm, bytes(certificate_chain)) is not True:
            return False
        if canonical_bytes(policy) != before:
            raise ValueError('SIGNER_KEY_INSPECTOR_POLICY_CHANGED')
        return self._native(owned.model_copy(deep=True), algorithm, bytes(certificate_chain)) is True
