"""Offline signer deployment file preflight; never native qualification."""
from hashlib import sha256
from datetime import datetime, timezone

from photo_worker.contracts.base import parse_json
from photo_worker.contracts.provenance import SigningPolicy, decode_provenance
from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles
from photo_worker.operations.provenance.certificate_inspector import check_certificate_structure
from .signer_deployment import _read, load_signer_deployment
from photo_worker.operations.provenance.cancellation import signing_task_scope


def verify_signer_deployment_files(root, runtime_lock, files, manifest='installation.json', *,
                                   check_active=lambda: None):
    """Return public pins only after every configured file matches installed bytes.

    Runtime files are passed as logical name -> absolute path by the operator.
    The PIN is deliberately not read and no native handle is opened here.
    """
    with signing_task_scope(check_active):
        if not isinstance(files, dict) or not files:
            raise ValueError('SIGNER_PREFLIGHT_FILES')
        deployment = load_signer_deployment(root, manifest)
        lock = parse_json(_read(root, runtime_lock, 1 << 20), max_bytes=1 << 20)
        policies = tuple(decode_provenance(raw, SigningPolicy, 1 << 20)
                         for raw in deployment.policies)
        sdk_sha = policies[0].sdk_lock_sha
        if any(policy.sdk_lock_sha != sdk_sha for policy in policies):
            raise ValueError('SIGNER_PREFLIGHT_MIXED_SDK')
        for policy in policies:
            check_certificate_structure(policy, deployment.algorithm,
                                        deployment.certificate_chain, datetime.now(timezone.utc))
        pinned = PinnedRuntimeFiles(lock, sdk_sha, files)
        pinned.check()
        return {'valid': True, 'sdk_lock_sha': sdk_sha,
                'qualification_sha': deployment.qualification_sha,
                'certificate_chain_sha': sha256(deployment.certificate_chain).hexdigest(),
                'trust_bundle_sha': sha256(deployment.trust_bundle).hexdigest(),
                'policy_count': len(policies), 'native_qualified': False}
