"""Install V23 Evaluate only from a deployment-signed measured runtime."""
from __future__ import annotations

from pathlib import Path

from photo_worker.models.install import verify_model

from .installation import pinned_learning_evaluation_installation
from .qualification import verify_learning_qualification


_BASE_MODELS = frozenset({'sfd', 'fan2d', 'identity512',
                          'lraspp_mobilenet_v3_large'})
_EFFECT_MODELS = {
    'restoration.denoise': 'scunet_color_real_psnr',
    'background.blur': 'depth_anything_v2_small',
}


def installed_cuda_device() -> tuple[str, int, int, int]:
    import torch
    if not torch.cuda.is_available():
        raise ValueError('LEARNING_CUDA_UNAVAILABLE')
    properties = torch.cuda.get_device_properties(torch.cuda.current_device())
    return (properties.name, properties.major, properties.minor,
            properties.total_memory)


def signed_pinned_learning_evaluation_installation(pool, registry, *,
    report_bytes: bytes, signature: bytes, trusted_public_key: bytes,
    trusted_issuer_id: str,
    package_root: Path, packages_lock: Path, evidence_root: Path,
    denoise_provider=None,
    depth_provider=None, background_verify_runtime=None,
    verify_deployment_files=None):
    """Verify exact report and native dependencies at startup and every task.

    The caller supplies only deployment-owned report/key paths. The current
    device and model bytes are read locally; a report cannot claim them alone.
    """
    if (pool is None or registry is None or not isinstance(report_bytes, bytes)
            or not isinstance(signature, bytes) or
            not isinstance(trusted_public_key, bytes) or
            not isinstance(trusted_issuer_id, str) or not trusted_issuer_id or
            (verify_deployment_files is not None and
             not callable(verify_deployment_files))):
        raise ValueError('LEARNING_QUALIFICATION_INVALID')
    report_bytes, signature, trusted_public_key = (
        bytes(report_bytes), bytes(signature), bytes(trusted_public_key))
    lock_sha = pool.lock.lock_id
    icc_sha = registry.resolve('work_rec2020_gamma24_v1').sha256
    package_root, packages_lock, evidence_root = (
        Path(package_root), Path(packages_lock), Path(evidence_root))

    def verify_runtime():
        if verify_deployment_files is not None and verify_deployment_files() is not None:
            raise ValueError('LEARNING_QUALIFICATION_FILES_CHANGED')
        if pool.lock.lock_id != lock_sha:
            raise ValueError('LEARNING_MODEL_LOCK_CHANGED')
        current_icc = registry.resolve('work_rec2020_gamma24_v1').sha256
        if current_icc != icc_sha:
            raise ValueError('LEARNING_ICC_CHANGED')
        proof = verify_learning_qualification(report_bytes, signature,
            trusted_public_key, trusted_issuer_id=trusted_issuer_id,
            model_lock_sha=lock_sha, icc_lock_sha=icc_sha,
            package_root=package_root, packages_lock=packages_lock,
            evidence_root=evidence_root,
            device=installed_cuda_device())
        entries = {entry.id: entry for entry in pool.lock.models}
        required = _BASE_MODELS | {
            model for effect, model in _EFFECT_MODELS.items()
            if effect in proof.effects}
        if not required <= entries.keys():
            raise ValueError('LEARNING_MODEL_QUALIFICATION_MISSING')
        for model_id in sorted(required):
            entry = entries[model_id]
            if (not entry.approved_for_install or
                    'cuda' not in entry.backends or
                    entry.backends['cuda'].status != 'passed' or
                    entry.backends['cuda'].report_sha256 is None):
                raise ValueError('LEARNING_MODEL_UNQUALIFIED')
            verify_model(pool.root / lock_sha / model_id, entry)
        if 'background.asset' in proof.effects and not callable(installed_background):
            raise ValueError('LEARNING_BACKGROUND_INSTALLATION_REQUIRED')
        return proof

    def verify_signed_background_decoder():
        if 'background.asset' not in verify_runtime().effects:
            raise ValueError('LEARNING_BACKGROUND_INSTALLATION_CHANGED')

    installed_background = (background_verify_runtime if
        background_verify_runtime is not None else verify_signed_background_decoder)
    proof = verify_runtime()
    def recheck():
        if verify_runtime() != proof:
            raise ValueError('LEARNING_QUALIFICATION_CHANGED')
    installed_denoise = denoise_provider
    if 'restoration.denoise' in proof.effects:
        from photo_worker.v2.render_native_base import NativeDenoiseProvider
        peak = proof.peak_for('restoration.denoise')
        if installed_denoise is None:
            installed_denoise = NativeDenoiseProvider(pool, backend='cuda',
                provider_peak=peak, verify_runtime=recheck)
        elif (not isinstance(installed_denoise, NativeDenoiseProvider) or
                installed_denoise.pool is not pool or
                installed_denoise.backend != 'cuda' or
                installed_denoise.peak() != peak):
            raise ValueError('LEARNING_DENOISE_INSTALLATION_CHANGED')
    installed_depth = depth_provider
    if 'background.blur' in proof.effects:
        from .depth import HoldoutDepthProvider
        peak = proof.peak_for('background.blur')
        if installed_depth is None:
            installed_depth = HoldoutDepthProvider.create(pool, backend='cuda',
                provider_peak=peak, verify_runtime=recheck)
        elif (not isinstance(installed_depth, HoldoutDepthProvider) or
                installed_depth.pool is not pool or
                installed_depth.backend != 'cuda' or
                dict(installed_depth.provider_peak) != peak):
            raise ValueError('LEARNING_DEPTH_INSTALLATION_CHANGED')
    return pinned_learning_evaluation_installation(pool, registry,
        verify_runtime=recheck, denoise_provider=installed_denoise,
        depth_provider=installed_depth,
        background_verify_runtime=installed_background if
            'background.asset' in proof.effects else background_verify_runtime,
        qualification=proof)
