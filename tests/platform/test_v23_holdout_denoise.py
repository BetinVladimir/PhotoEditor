from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.learning.denoise import open_holdout_denoise
from photo_worker.v2.render_native_base import NativeDenoiseProvider
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.image.prepare_denoise import prepare_denoise
from photo_worker.contracts.restoration import DenoiseParams
from photo_worker.runtime.arrays import read_array
from test_style_resolve import profile
from test_v23_learning_source import _fixture


def setup(tmp_path):
    artifacts, case = _fixture(tmp_path)
    root = tmp_path / 'scratch-denoise'
    root.mkdir()
    artifacts.scratch = ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0)
    style = profile().model_copy(deep=True)
    from photo_worker.contracts.style import Restoration
    style.extensions.restoration = Restoration(denoise='scunet_color_real_psnr',
        denoise_strength=.8, upscale='off', sensor_dust='off')
    style.strength = .5
    events = []
    engine = SimpleNamespace(infer=lambda value: (value * np.float32(.95)).astype(np.float32))

    @contextmanager
    def acquire(model, backend):
        events.append(('open', model, backend))
        try:
            yield engine
        finally:
            events.append(('close', model, backend))

    pool = SimpleNamespace(lock=SimpleNamespace(lock_id='c'*64,
        models=[SimpleNamespace(id='scunet_color_real_psnr')]), acquire=acquire)
    provider = NativeDenoiseProvider(pool, backend='cuda',
        provider_peak=dict(ram_bytes=0, scratch_bytes=0, vram_bytes=1, output_bytes=0),
        verify_runtime=lambda: events.append('verify'))
    execution = dict(backend='cuda', resource_estimate=dict(ram_bytes=2**36,
        scratch_bytes=2**36, vram_bytes=2**32, output_bytes=8388608))
    return artifacts, case, style, provider, execution, events, engine


def test_production_denoise_parity_and_one_strength(tmp_path):
    artifacts, case, style, provider, execution, events, engine = setup(tmp_path)
    try:
        params = DenoiseParams(image=case.working_image, strength=.4, model_lock_id='c'*64)
        with prepare_denoise(artifacts.rgb, params, engine, artifacts.scratch) as prepared:
            with read_array(prepared.path, artifacts.rgb.shape, 'float32', lambda: None,
                            unit_interval=True) as pixels:
                expected = pixels.copy()
        with open_holdout_denoise(artifacts, case.working_image, artifacts.rgb, style,
                provider, model_lock_sha='c'*64, execution_profile=execution) as pixels:
            assert np.array_equal(pixels.view(np.uint32), expected.view(np.uint32))
            assert pixels is not artifacts.rgb
        assert events == ['verify', ('open', 'scunet_color_real_psnr', 'cuda'),
                          'verify', ('close', 'scunet_color_real_psnr', 'cuda'), 'verify']
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()


@pytest.mark.parametrize('mode', ['missing', 'lock', 'model', 'upscale', 'dust',
                                  'profile', 'ram', 'scratch', 'vram'])
def test_denoise_refusals_before_model_io(tmp_path, mode):
    artifacts, case, style, provider, execution, events, _ = setup(tmp_path)
    lock = 'c'*64
    if mode == 'missing': provider = None
    if mode == 'lock': lock = 'd'*64
    if mode == 'model': provider.pool.lock.models = []
    if mode == 'upscale': style.extensions.restoration.upscale = '2x'
    if mode == 'dust': style.extensions.restoration.sensor_dust = 'review'
    if mode == 'profile': execution = None
    for key in ('ram', 'scratch', 'vram'):
        if mode == key: execution['resource_estimate'][key + '_bytes'] = 0
    try:
        with pytest.raises((ValueError, OperationFailure)):
            with open_holdout_denoise(artifacts, case.working_image, artifacts.rgb, style,
                    provider, model_lock_sha=lock, execution_profile=execution):
                pytest.fail('invalid restoration accepted')
        assert events == []
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()


def test_denoise_cancellation_releases_model_and_scratch(tmp_path):
    artifacts, case, style, provider, execution, events, engine = setup(tmp_path)
    live = True
    def infer(value):
        nonlocal live
        live = False
        return value.copy()
    def active():
        if not live: raise RuntimeError('cancelled')
    engine.infer = infer
    try:
        with pytest.raises(RuntimeError, match='cancelled'):
            with open_holdout_denoise(artifacts, case.working_image, artifacts.rgb, style,
                    provider, model_lock_sha='c'*64, execution_profile=execution, check_active=active):
                pytest.fail('cancelled result yielded')
        assert events[-1] == ('close', 'scunet_color_real_psnr', 'cuda')
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()


@pytest.mark.parametrize('phase', ['verify', 'infer', 'consumer', 'close'])
def test_model_lock_drift_cannot_yield_or_accept_render(tmp_path, phase):
    artifacts, case, style, provider, execution, events, engine = setup(tmp_path)
    pool = provider.pool
    original_acquire = pool.acquire
    verified = []

    def verify():
        verified.append(True)
        if phase == 'verify':
            pool.lock.lock_id = 'd'*64

    @contextmanager
    def acquire(model, backend):
        with original_acquire(model, backend) as value:
            yield value
        if phase == 'close':
            pool.lock.lock_id = 'd'*64

    def infer(value):
        if phase == 'infer':
            pool.lock.lock_id = 'd'*64
        return value.copy()

    pool.acquire = acquire
    engine.infer = infer
    provider = NativeDenoiseProvider(pool, backend='cuda', provider_peak=provider.peak(),
                                    verify_runtime=verify)
    yielded = False
    try:
        with pytest.raises(ValueError, match='HOLDOUT_DENOISE_INSTALLATION_CHANGED'):
            with open_holdout_denoise(artifacts, case.working_image, artifacts.rgb, style,
                    provider, model_lock_sha='c'*64, execution_profile=execution):
                yielded = True
                if phase == 'consumer':
                    pool.lock.lock_id = 'd'*64
        assert yielded == (phase in ('consumer', 'close'))
        if phase == 'verify':
            assert events == []
        else:
            assert events[-1] == ('close', 'scunet_color_real_psnr', 'cuda')
        assert list(artifacts.scratch.directory.iterdir()) == []
    finally:
        artifacts.scratch.cleanup()
