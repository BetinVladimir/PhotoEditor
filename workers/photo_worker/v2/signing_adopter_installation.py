"""Process-owned V31 verifier installation for the scheduler adopter.

The adopter can validate signed exports without a PKCS#11 signing handle.
Native SDK identity, certificate/trust status and binary binding remain live
operator-supplied authorities; a deployment document is only a pin catalogue.
"""
from photo_worker.contracts.provenance import SigningPolicy, decode_provenance
from photo_worker.operations.provenance.certificate_inspector import CertificateBoundInstallationInspector
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.operations.provenance.installation_files import FileBoundInstallationInspector, PinnedRuntimeFiles
from photo_worker.operations.provenance.native_operator import NativeSigningDelegate
from photo_worker.runtime.signer_deployment import SignerDeployment, load_signer_deployment


def build_signing_adopter_operator(deployment, *, sdk, runtime_files,
                                   native_authority, native_inspector,
                                   verify_binding, check_active):
    if (not isinstance(deployment, SignerDeployment)
            or not isinstance(runtime_files, PinnedRuntimeFiles)
            or not all(callable(item) for item in
                       (native_authority, native_inspector, verify_binding, check_active))
            or not callable(getattr(getattr(sdk, 'Context', None), 'from_dict', None))
            or not callable(getattr(sdk, 'Reader', None))):
        raise ValueError('SIGNING_ADOPTER_INSTALLATION')
    def active():
        if check_active() is False:
            raise RuntimeError('SIGNING_ADOPTER_INACTIVE')
    active()
    policies = tuple(decode_provenance(raw, SigningPolicy, 1 << 20)
                     for raw in deployment.policies)
    if (not policies or any(policy.sdk_lock_sha != runtime_files.sdk_lock_sha
                            for policy in policies)):
        raise ValueError('SIGNING_ADOPTER_RUNTIME_PIN')
    runtime_files.check()
    def guarded(callback):
        def invoke(*args):
            active()
            result = callback(*args)
            active()
            return result
        return invoke
    inspector = FileBoundInstallationInspector(runtime_files,
        CertificateBoundInstallationInspector(guarded(native_inspector),
            trust_bundle=deployment.trust_bundle))
    def refuse_sign(*_args):
        raise RuntimeError('SIGNING_ADOPTER_CANNOT_SIGN')
    delegate = NativeSigningDelegate(sdk, runtime_files.sdk_lock_sha,
        deployment.trust_bundle, deployment.certificate_chain,
        deployment.algorithm, refuse_sign, inspector, guarded(verify_binding),
        dict(deployment.timestamp_services), deployment.network_hosts)
    def authority(policy, digest):
        return guarded(native_authority)(policy, digest, deployment.qualification_sha)
    operator = InstalledSigningOperator(deployment.policies, authority, delegate)
    for policy in policies:
        active()
        operator.check_installation(policy)
        with operator.open_context(policy):
            active()
    return operator


def run_deployed_signing_adopter(config, stop, deployment_root, *, sdk,
                                 runtime_files, native_authority,
                                 native_inspector, verify_binding,
                                 manifest='installation.json'):
    """Run the ordinary V2 adopter with one pinned V31 verification catalogue."""
    from threading import Event
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    from .service import run_adopter
    if (not isinstance(stop, Event) or stop.is_set()
            or not isinstance(runtime_files, PinnedRuntimeFiles)
            or not all(callable(item) for item in
                       (native_authority, native_inspector, verify_binding))):
        raise ValueError('SIGNING_ADOPTER_STOP')
    def active():
        if stop.is_set():
            raise RuntimeError('SIGNING_ADOPTER_INACTIVE')
    with signing_task_scope(active):
        deployment = load_signer_deployment(deployment_root, manifest)
        operator = build_signing_adopter_operator(deployment, sdk=sdk,
            runtime_files=runtime_files, native_authority=native_authority,
            native_inspector=native_inspector, verify_binding=verify_binding,
            check_active=lambda: not stop.is_set())
    return run_adopter(config, stop, signing_operator=operator, signing_sdk=sdk)
