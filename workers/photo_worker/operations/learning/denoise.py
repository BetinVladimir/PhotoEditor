"""Pinned same-canvas restoration before holdout color and beauty."""
from contextlib import contextmanager
from copy import deepcopy

from photo_worker.contracts.restoration import DenoiseParams
from photo_worker.operations.image.prepare_denoise import prepare_denoise
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.render_native_base import NativeDenoiseProvider


@contextmanager
def open_holdout_denoise(artifacts, source_ref, rgb, style, provider, *,
                         model_lock_sha, execution_profile, check_active=lambda: None):
    check_active()
    restoration = style.extensions.restoration
    if restoration is None:
        yield rgb
        check_active()
        return
    restoration = restoration.model_copy(deep=True)
    strength = restoration.denoise_strength * style.strength
    if restoration.upscale != 'off' or restoration.sensor_dust != 'off':
        raise ValueError('HOLDOUT_COMPLETE_RESTORATION_REQUIRED')
    if restoration.denoise == 'off':
        yield rgb
        check_active()
        return
    if (not isinstance(provider, NativeDenoiseProvider)
            or provider.pool.lock.lock_id != model_lock_sha
            or restoration.denoise not in {entry.id for entry in provider.pool.lock.models}):
        raise ValueError('HOLDOUT_DENOISE_INSTALLATION_REQUIRED')
    profile = deepcopy(execution_profile)
    estimate = profile.get('resource_estimate') if isinstance(profile, dict) else None
    if (not isinstance(profile, dict) or profile.get('backend') != provider.backend
            or not isinstance(estimate, dict)
            or any(type(estimate.get(key)) is not int or estimate[key] < 0
                   for key in ('ram_bytes', 'scratch_bytes', 'vram_bytes', 'output_bytes'))):
        raise OperationFailure('CONFIG_INVALID')
    h, w = rgb.shape[:2]
    pixels = h * w
    roi = min(pixels, 4194304)
    peak = provider.peak()
    # Retain the entire learning workspace/beauty envelope while denoising.
    required = dict(ram_bytes=96*pixels + 192*min(h, 640)*min(w, 640) + 268435456,
                    scratch_bytes=128*pixels + 64*16*roi + 536870912 + 2097152,
                    vram_bytes=0, output_bytes=0)
    for key, value in peak.items():
        required[key] += value
    for key, code in (('ram_bytes', 'RAM_ADMISSION_EXCEEDED'),
                      ('scratch_bytes', 'SCRATCH_ADMISSION_EXCEEDED'),
                      ('vram_bytes', 'VRAM_ADMISSION_EXCEEDED'),
                      ('output_bytes', 'OUTPUT_ADMISSION_EXCEEDED')):
        if estimate[key] < required[key]:
            raise OperationFailure(code)
    params = DenoiseParams(image=source_ref, strength=strength, model_lock_id=model_lock_sha)
    pool, backend = provider.pool, provider.backend
    verify_runtime, acquire = provider.verify_runtime, provider.pool.acquire

    def pinned():
        check_active()
        if (provider.pool is not pool or provider.backend != backend
                or provider.verify_runtime != verify_runtime or pool.acquire != acquire
                or pool.lock.lock_id != model_lock_sha
                or params.model_id not in {entry.id for entry in pool.lock.models}
                or provider.peak() != peak):
            raise ValueError('HOLDOUT_DENOISE_INSTALLATION_CHANGED')

    def verify():
        pinned()
        verify_runtime()
        pinned()

    verify()
    with acquire(params.model_id, backend) as engine:
        pinned()
        with prepare_denoise(rgb, params, engine, artifacts.scratch,
                             check_active=check_active) as prepared:
            with read_array(prepared.path, (h, w, 3), 'float32', check_active,
                            unit_interval=True) as result:
                verify()
                yield result
                pinned()
    verify()
