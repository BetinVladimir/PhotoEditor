"""Same-canvas native SCUNet stage before Region color/local/beauty."""
from contextlib import contextmanager
from photo_worker.contracts.restoration import DenoiseParams
from photo_worker.operations.image.prepare_denoise import prepare_denoise
from photo_worker.runtime.arrays import read_array


@contextmanager
def prepare_region_denoise(artifacts,source_ref,rgb,style,provider,*,backend,model_lock_sha,
                           verify_runtime,check_active=lambda:None):
    restoration=style.extensions.restoration
    if restoration is None or restoration.denoise=='off':
        yield rgb
        check_active()
        return
    if provider is None or provider.backend!=backend or provider.pool.lock.lock_id!=model_lock_sha:
        raise ValueError('REGION_RESTORATION_UNAVAILABLE')
    params=DenoiseParams(image=source_ref,strength=restoration.denoise_strength*style.strength,
        model_lock_id=model_lock_sha)
    verify_runtime();provider.verify_runtime();check_active()
    with provider.pool.acquire(params.model_id,backend) as engine:
        with prepare_denoise(rgb,params,engine,artifacts.scratch,check_active=check_active) as prepared:
            with read_array(prepared.path,rgb.shape,'float32',check_active,unit_interval=True) as denoised:
                verify_runtime();provider.verify_runtime();check_active()
                yield denoised
                check_active();verify_runtime();provider.verify_runtime()
