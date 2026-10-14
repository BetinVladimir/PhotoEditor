"""Explicit signer-only process composition; no automatic general CPU fallback.

Provisioning supplies policies, qualified native delegate and independent live
native authority. PostgreSQL qualification alone cannot attest native behavior.
"""
from hashlib import sha256
import re
from uuid import UUID

from photo_worker.contracts.provenance import SigningPolicy, decode_provenance
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.operations.provenance.execute import signer_operations
from .executor import Executor


def build_signer_executor(database, worker_id, config, policies,
                          qualification_sha, native_authority, delegate, *, on_operator=None):
    """Bind native and authenticated registration authority to every SDK call.

    qualification_sha identifies an operator-approved immutable report, not a
    worker-generated success flag. This function neither provisions nor extends
    it. Policy/SDK/key rotation requires constructing a new installation.
    """
    if on_operator is not None and not callable(on_operator):
        raise ValueError('SIGNER_PROCESS_OPERATOR_CALLBACK')
    if (not isinstance(worker_id, UUID) or worker_id.int == 0
            or not isinstance(qualification_sha, str)
            or re.fullmatch(r'[0-9a-f]{64}', qualification_sha) is None
            or not callable(native_authority)
            or not isinstance(policies, (list, tuple)) or not 1 <= len(policies) <= 256):
        raise ValueError('SIGNER_PROCESS_INSTALLATION')
    if any(not isinstance(raw, bytes) for raw in policies) or sum(len(raw) for raw in policies) > 8 << 20:
        raise ValueError('SIGNER_PROCESS_POLICY_BYTES')
    owned = tuple(bytes(raw) for raw in policies)
    decoded = tuple(decode_provenance(raw, SigningPolicy, 1 << 20) for raw in owned)
    # A registration describes one native SDK/certificate/trust installation.
    pins = {(p.sdk_lock_sha, p.certificate_chain_sha, p.trust_bundle_sha) for p in decoded}
    if len(pins) != 1:
        raise ValueError('SIGNER_PROCESS_MIXED_INSTALLATIONS')
    installed = {sha256(raw).hexdigest(): raw for raw in owned}
    def authority(policy, policy_sha):
        raw = installed.get(policy_sha)
        if raw is None:
            return False
        exact = decode_provenance(raw, SigningPolicy, 1 << 20)
        if policy != exact:
            return False
        # Check before and after native authority in case revocation occurs
        # while inspecting native libraries/certificates/key availability.
        def registered():
            # Qualification pins alone do not prove signer-only operation
            # isolation. Recheck both declarations on every authority boundary,
            # including callbacks after the executor has already been built.
            if database.runtime_operations(worker_id, 'cpu') != frozenset({'provenance.sign'}):
                return False
            if database.signer_runtime_ready(worker_id, exact.sdk_lock_sha,
                    exact.certificate_chain_sha, exact.trust_bundle_sha, qualification_sha) is not True:
                return False
            return database.runtime_operations(worker_id, 'cpu') == frozenset({'provenance.sign'})
        if not registered():
            return False
        if native_authority(exact.model_copy(deep=True), policy_sha, qualification_sha) is not True:
            return False
        return registered()
    operator = InstalledSigningOperator(owned, authority, delegate)
    if database.runtime_operations(worker_id, 'cpu') != frozenset({'provenance.sign'}):
        raise ValueError('SIGNER_PROCESS_OPERATION_ISOLATION')
    for policy in decoded:
        operator.check_installation(policy)
    executor = Executor(database, worker_id, signer_operations(config, operator))
    if on_operator is not None:on_operator(operator)
    return executor


def build_native_signer_executor(database, worker_id, config, policies,
                                 qualification_sha, native_authority, *, sdk,
                                 trust_bundle, certificate_chain, algorithm,
                                 sign_payload, inspect_installation, verify_binding,
                                 timestamp_services=None, network_hosts=(), on_operator=None):
    """Compose the native callback delegate with SQL and independent authority.

    All native dependencies originate in trusted process construction. No task
    can choose SDK imports, key callbacks, trust stores or timestamp endpoints.
    SDK/certificate/trust qualification is still independently mandatory.
    """
    from photo_worker.operations.provenance.native_operator import NativeSigningDelegate
    if (not isinstance(policies, (list, tuple)) or not 1 <= len(policies) <= 256
            or any(not isinstance(raw, bytes) for raw in policies)
            or sum(len(raw) for raw in policies) > 8 << 20):
        raise ValueError('SIGNER_PROCESS_POLICY_BYTES')
    owned = tuple(bytes(raw) for raw in policies)
    decoded = tuple(decode_provenance(raw, SigningPolicy, 1 << 20) for raw in owned)
    pins = {(p.sdk_lock_sha, p.certificate_chain_sha, p.trust_bundle_sha) for p in decoded}
    if len(pins) != 1:
        raise ValueError('SIGNER_PROCESS_MIXED_INSTALLATIONS')
    delegate = NativeSigningDelegate(sdk, decoded[0].sdk_lock_sha, trust_bundle,
        certificate_chain, algorithm, sign_payload, inspect_installation,
        verify_binding, timestamp_services, network_hosts)
    return build_signer_executor(database, worker_id, config, owned,
        qualification_sha, native_authority, delegate, on_operator=on_operator)


def build_pkcs11_signer_executor(database, worker_id, config, policies,
                                 qualification_sha, native_authority, *, sdk,
                                 trust_bundle, certificate_chain, algorithm,
                                 pkcs11, library, key_handle, token_serial, key_id,
                                 pin_supplier, native_inspector, verify_binding,
                                 timestamp_services=None, network_hosts=(),
                                 check_active=lambda: None, runtime_files=None, on_operator=None):
    """Concrete RSA-PSS/ECDSA/Ed25519 composition; no private key/PIN in task JSON."""
    from photo_worker.operations.provenance.pkcs11_key import PKCS11RSAKey
    from photo_worker.operations.provenance.pkcs11_ec_key import PKCS11ECKey
    from photo_worker.operations.provenance.pkcs11_ed25519_key import PKCS11Ed25519Key
    from photo_worker.operations.provenance.key_inspector import KeyBoundInstallationInspector
    if algorithm not in ('PS256', 'PS384', 'PS512', 'ES256', 'ES384', 'ES512', 'ED25519'):
        raise ValueError('SIGNER_PROCESS_PKCS11_ALGORITHM')
    if (not isinstance(policies, (list, tuple)) or not 1 <= len(policies) <= 256
            or any(not isinstance(raw, bytes) for raw in policies)
            or sum(len(raw) for raw in policies) > 8 << 20):
        raise ValueError('SIGNER_PROCESS_POLICY_BYTES')
    owned = tuple(bytes(raw) for raw in policies)
    decoded = tuple(decode_provenance(raw, SigningPolicy, 1 << 20) for raw in owned)
    if any(policy.key_handle != key_handle for policy in decoded):
        raise ValueError('SIGNER_PROCESS_PKCS11_KEY_BINDING')
    if runtime_files is not None:
        from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles, FileBoundInstallationInspector
        if (not isinstance(runtime_files, PinnedRuntimeFiles)
                or any(policy.sdk_lock_sha != runtime_files.sdk_lock_sha for policy in decoded)):
            raise ValueError('SIGNER_PROCESS_RUNTIME_FILES')
        native_inspector = FileBoundInstallationInspector(runtime_files, native_inspector)
        runtime_files.check()
    provider = (PKCS11RSAKey if algorithm.startswith('PS') else
                PKCS11ECKey if algorithm.startswith('ES') else PKCS11Ed25519Key)
    key = provider(pkcs11, library, key_handle=key_handle,
        token_serial=token_serial, key_id=key_id, certificate_chain=certificate_chain,
        pin_supplier=pin_supplier, check_active=check_active)
    return build_native_signer_executor(database, worker_id, config, owned,
        qualification_sha, native_authority, sdk=sdk, trust_bundle=trust_bundle,
        certificate_chain=certificate_chain, algorithm=algorithm,
        sign_payload=key.sign_payload,
        inspect_installation=KeyBoundInstallationInspector(key, native_inspector),
        verify_binding=verify_binding, timestamp_services=timestamp_services,
        network_hosts=network_hosts, on_operator=on_operator)
