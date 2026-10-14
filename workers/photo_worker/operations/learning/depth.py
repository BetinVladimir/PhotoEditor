"""Explicit qualified depth dependency for same-canvas holdout blur."""
from contextlib import contextmanager
from dataclasses import dataclass
from copy import deepcopy
import numpy as np
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.operations.depth.prepare import normalize_depth
from photo_worker.runtime.executor import OperationFailure


@dataclass(frozen=True)
class HoldoutDepthProvider:
    pool: object
    backend: str
    verify_runtime: object
    provider_peak: tuple

    def __post_init__(self):
        from photo_worker.v2.render_resources import render_resources
        if (self.backend != 'cuda' or not callable(self.verify_runtime)
                or not isinstance(self.provider_peak, tuple) or len(self.provider_peak) != 4
                or any(not isinstance(item, tuple) or len(item) != 2 for item in self.provider_peak)):
            raise ValueError('HOLDOUT_DEPTH_INSTALLATION_REQUIRED')
        peak = dict(self.provider_peak)
        if len(peak) != 4:
            raise ValueError('HOLDOUT_DEPTH_INSTALLATION_REQUIRED')
        render_resources(1, 1, mask_refs=0, mask_nodes=0, provider_peak=peak)

    @classmethod
    def create(cls, pool, *, backend, verify_runtime, provider_peak):
        from photo_worker.v2.render_resources import render_resources
        if backend != 'cuda' or not callable(verify_runtime):
            raise ValueError('HOLDOUT_DEPTH_INSTALLATION_REQUIRED')
        render_resources(1, 1, mask_refs=0, mask_nodes=0, provider_peak=provider_peak)
        return cls(pool, backend, verify_runtime, tuple(sorted(deepcopy(provider_peak).items())))


@contextmanager
def open_holdout_depth(source, provider, scratch, *, model_lock_sha,
                        execution_profile, check_active=lambda: None):
    model = 'depth_anything_v2_small'
    if not isinstance(provider, HoldoutDepthProvider):
        raise ValueError('HOLDOUT_DEPTH_INSTALLATION_REQUIRED')
    pool, backend, verify = provider.pool, provider.backend, provider.verify_runtime
    acquire, peak = pool.acquire, provider.provider_peak
    def pinned():
        check_active()
        if (pool.lock.lock_id != model_lock_sha or model not in {x.id for x in pool.lock.models}
                or provider.pool is not pool or provider.backend != backend
                or provider.verify_runtime != verify or pool.acquire != acquire
                or provider.provider_peak != peak):
            raise ValueError('HOLDOUT_DEPTH_INSTALLATION_CHANGED')
    pinned()
    h, w = source.rgb.shape[:2]
    for y in range(0, h, 512):
        check_active()
        if source.alpha is not None and np.any(source.alpha[y:y+512] == 0):
            raise ValueError('HOLDOUT_DEPTH_INVALID_COVERAGE')
    profile = deepcopy(execution_profile)
    estimate = profile.get('resource_estimate') if isinstance(profile, dict) else None
    if (not isinstance(profile, dict) or profile.get('backend') != backend
            or not isinstance(estimate, dict)):
        raise OperationFailure('CONFIG_INVALID')
    pixels = h*w
    radius = int(np.ceil(.02*max(h,w)))
    halo = int(np.ceil(4*max(radius/3,.5)))+2 if radius else 0
    support = min(h,512+2*halo)*min(w,512+2*halo)
    required = dict(ram_bytes=128*pixels+256*support+268435456,
        scratch_bytes=144*pixels+64*16*min(pixels,4194304)+536870912+2097152,
        vram_bytes=0, output_bytes=0)
    for key, value in provider.provider_peak:
        required[key] += value
    for key in required:
        if type(estimate.get(key)) is not int or estimate[key] < required[key]:
            raise OperationFailure('CONFIG_INVALID')
    verify(); pinned()
    srgb, _ = work_to_model(source.rgb)
    pinned()
    with acquire(model, backend) as engine:
        pinned()
        raw = engine.infer(srgb)
        pinned()
    del srgb
    pinned()
    depth, flat = normalize_depth(raw, w, h)
    del raw
    if flat:
        raise ValueError('HOLDOUT_DEPTH_NOT_MEASURABLE')
    verify(); pinned()
    with scratch.reserve(4*pixels+16384) as owner:
        mapped = np.lib.format.open_memmap(owner.path('holdout-depth', '.npy'),
                    mode='w+', dtype='<f4', shape=(h,w))
        try:
            for y in range(0,h,512):
                pinned(); mapped[y:y+512] = depth[y:y+512]
            del depth
            mapped.flush(); owner.validate(); mapped.flags.writeable=False
            pinned()
            yield mapped
            pinned()
        finally:
            mapped._mmap.close()
    verify(); pinned()
