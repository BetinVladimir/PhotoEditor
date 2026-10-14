"""Pinned per-operation C2PA settings, supplied by process provisioning.

Reference: contentauth/c2pa-python docs/context-settings.md. This factory does
not attest native SDK identity, certificate validity or network enforcement;
InstalledSigningOperator's live authority remains mandatory around its use.
"""
from contextlib import contextmanager
from hashlib import sha256
import re

from photo_worker.contracts.provenance import SigningPolicy


class PinnedSigningContext:
    def __init__(self, sdk, sdk_lock_sha, trust_bundle, network_hosts=()):
        if (not isinstance(sdk_lock_sha, str)
                or re.fullmatch(r'[0-9a-f]{64}', sdk_lock_sha) is None
                or not isinstance(trust_bundle, bytes)
                or not 1 <= len(trust_bundle) <= 1 << 20
                or not isinstance(network_hosts, (list, tuple))
                or len(network_hosts) > 64):
            raise ValueError('SIGNER_CONTEXT_INSTALLATION')
        # Exact PEM bytes are the policy pin; no trimming/re-encoding before hash.
        try:
            trust = trust_bundle.decode('ascii')
        except UnicodeDecodeError as exc:
            raise ValueError('SIGNER_CONTEXT_TRUST') from exc
        if ('-----BEGIN CERTIFICATE-----' not in trust
                or '-----END CERTIFICATE-----' not in trust
                or 'PRIVATE KEY' in trust):
            raise ValueError('SIGNER_CONTEXT_TRUST')
        hosts = []
        for host in network_hosts:
            if (not isinstance(host, str) or len(host) > 253
                    or not re.fullmatch(r'[a-z0-9]+(?:[.-][a-z0-9]+)*', host)
                    or any(not 1 <= len(label) <= 63 or label.startswith('-')
                           or label.endswith('-') for label in host.split('.'))):
                raise ValueError('SIGNER_CONTEXT_NETWORK')
            hosts.append(host)
        if len(set(hosts)) != len(hosts):
            raise ValueError('SIGNER_CONTEXT_NETWORK')
        self._sdk = sdk
        self._sdk_lock_sha = sdk_lock_sha
        self._trust = trust
        self._trust_sha = sha256(trust_bundle).hexdigest()
        self._hosts = tuple(sorted(hosts))

    @contextmanager
    def open_context(self, policy):
        if (not isinstance(policy, SigningPolicy)
                or policy.sdk_lock_sha != self._sdk_lock_sha
                or policy.trust_bundle_sha != self._trust_sha):
            raise ValueError('SIGNER_CONTEXT_POLICY')
        owned = policy.model_copy(deep=True)
        if ((owned.revocation_mode == 'required' or owned.timestamp_mode == 'required')
                and not self._hosts):
            raise ValueError('SIGNER_CONTEXT_NETWORK_REQUIRED')
        # Fresh dict and native context per call. No process-global settings or
        # developer allowed_list bypass, and no signer/private key in settings.
        settings = {
            'version': 1,
            'trust': {'trust_anchors': self._trust},
            'core': {'allowed_network_hosts': list(self._hosts),
                     'backing_store_memory_threshold_in_mb': 64},
            'verify': {'verify_after_reading': True, 'verify_after_sign': True,
                       'verify_trust': True, 'verify_timestamp_trust': True,
                       'remote_manifest_fetch': False,
                       'ocsp_fetch': owned.revocation_mode == 'required'},
            'builder': {'thumbnail': {'enabled': False}},
            'signer': None,
        }
        with self._sdk.Context.from_dict(settings) as context:
            yield context
