"""Pinned native Region startup; deployment files never select executable code.

Catalog membership is installation evidence only. SQLRegionAdmission still
requires the live task-scoped policy/report and exact server-selected sample.
"""
from pathlib import Path
from photo_worker.contracts.base import Contract, SHA256, parse_json
from photo_worker.operations.region.calibration import Qualification, Manifest, PinnedRegionCalibrationCatalog, _read


class RegionInstallationConfig(Contract):
    qualification: Qualification
    runtime_path: str
    calibration_path: str
    calibration_sha: SHA256


def region_installation_from_env(env, resource_class, backend):
    from .service import WorkerConfigurationError
    enabled=env.get('PHOTO_REGION_RUNTIME_ENABLED','false')
    path=env.get('PHOTO_REGION_INSTALLATION_FILE')
    digest=env.get('PHOTO_REGION_INSTALLATION_SHA')
    if enabled not in ('true','false'):
        raise WorkerConfigurationError('Invalid Region enablement')
    if enabled=='false':
        if path or digest:raise WorkerConfigurationError('Region pins require enablement')
        return None
    try:
        from hashlib import sha256
        if resource_class!='gpu' or backend not in ('cuda','mps') or not path or not digest:
            raise ValueError('Region requires GPU-class CUDA/Metal worker and pins')
        raw=_read(Path(path))
        if sha256(raw).hexdigest()!=digest:raise ValueError('Region installation pin mismatch')
        installation=RegionInstallationConfig.model_validate(parse_json(raw))
        if installation.qualification.backend!=backend:
            raise ValueError('Region backend mismatch')
        for name in ('runtime_path','calibration_path'):
            if not Path(getattr(installation,name)).is_absolute():raise ValueError('Region paths must be absolute')
        return installation
    except (ValueError,OSError) as error:
        raise WorkerConfigurationError(str(error)) from None


def configured_region_renderer(config,pool):
    from .service import WorkerConfigurationError, _model_backend_enabled
    from photo_worker.operations.region.installation import install_native_region_color
    installation=config.region_installation
    if installation is None:return None
    try:
        catalog=PinnedRegionCalibrationCatalog.load(Path(installation.calibration_path),installation.calibration_sha)
        qualification=installation.qualification.model_copy(deep=True)
        def verify(expected):
            catalog.verify_installed()
            manifest=Manifest.model_validate(parse_json(catalog._raw))
            return expected==qualification and any(p.qualification==expected for p in manifest.profiles)
        if not verify(qualification):raise ValueError('Region installation has no calibrated workload')
        if config.icc_root is None:raise ValueError('Region requires installed ICC registry')
        if pool is not None and pool.lock.lock_id!=qualification.model_lock_sha:
            raise ValueError('Region model lock mismatch')
        def available(models):
            if pool is None:return False
            installed={entry.id for entry in pool.lock.models}
            return models<=installed and all(_model_backend_enabled(pool,m,config.backend) for m in models)
        denoise=None
        if available({'scunet_color_real_psnr'}):
            from photo_worker.v2.render_native_base import NativeDenoiseProvider
            # Use only measured deployment peaks, never a device-memory guess.
            calibrated=Manifest.model_validate(parse_json(catalog._raw))
            samples=[sample for profile in calibrated.profiles
                if profile.qualification==qualification for sample in profile.samples]
            peak={key:max(getattr(sample.operator_peak,key) for sample in samples)
                for key in ('ram_bytes','scratch_bytes','vram_bytes','output_bytes')}
            denoise=NativeDenoiseProvider(pool,backend=config.backend,provider_peak=peak,
                verify_runtime=lambda:verify(qualification))
        return install_native_region_color(qualification=qualification,icc_root=config.icc_root,
            verify_qualification=verify,runtime_path=Path(installation.runtime_path),calibration_catalog=catalog,
            max_input_bytes=config.max_download_bytes,
            person_pool=pool if available({'lraspp_mobilenet_v3_large'}) else None,
            portrait_pool=pool if available({'sfd','fan2d','bisenet_face19'}) else None,
            denoise_provider=denoise,
            inpaint_pool=pool if available({'big_lama'}) else None,
            generation_pool=pool if available({'sd15_inpaint'}) else None,
            masking_pool=pool if available({'segformer_b0_ade','bisenet_face19','mobilesam'}) else None,
            upscale_pool=pool if available({'realesrgan_x2','realesrgan_x4'}) else None)
    except (ValueError,OSError) as error:
        raise WorkerConfigurationError('Region installation unavailable: '+str(error)) from None
