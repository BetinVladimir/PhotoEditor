from contextlib import contextmanager
from types import SimpleNamespace
import cv2
import numpy as np
import pytest
from photo_worker.operations.depth.prepare import normalize_depth
from photo_worker.operations.learning.depth import HoldoutDepthProvider, open_holdout_depth
from test_v23_holdout_denoise import setup


@pytest.mark.parametrize('flat', [False, True])
def test_shared_depth_normalization_preserves_native_math(flat):
    raw = np.ones((4,5),np.float32) if flat else np.arange(20,dtype=np.float32).reshape(4,5)
    resized = cv2.resize(raw,(13,9),interpolation=cv2.INTER_CUBIC)
    lo,hi = float(resized.min()),float(resized.max())
    expected = np.zeros((9,13),np.float32) if flat or hi-lo<=1e-12 else ((resized-lo)/(hi-lo)).astype(np.float32)
    actual, review = normalize_depth(raw,13,9)
    assert np.array_equal(actual.view(np.uint32),expected.view(np.uint32))
    assert review == flat


@pytest.mark.parametrize('mode', ['valid','missing','lock','transparent','flat','cancel','drift'])
def test_holdout_depth_scope_and_refusals(tmp_path, mode):
    artifacts,case,style,denoise,execution,events,engine = setup(tmp_path)
    pool = denoise.pool
    pool.lock.models = [SimpleNamespace(id='depth_anything_v2_small')]
    live = True
    def active():
        if not live: raise RuntimeError('cancelled')
    def infer(value):
        nonlocal live
        if mode == 'cancel': live = False
        if mode == 'drift': pool.lock.lock_id = 'd'*64
        return np.ones((4,5),np.float32) if mode == 'flat' else np.arange(20,dtype=np.float32).reshape(4,5)
    engine.infer = infer
    provider = HoldoutDepthProvider.create(pool,backend='cuda',verify_runtime=lambda: events.append('verify'),
        provider_peak=dict(ram_bytes=0,scratch_bytes=0,vram_bytes=1,output_bytes=0))
    source = SimpleNamespace(rgb=artifacts.rgb,alpha=np.full(artifacts.rgb.shape[:2],10000,np.uint16))
    if mode == 'transparent': source.alpha[0,0] = 0
    if mode == 'missing': provider = None
    lock = 'd'*64 if mode == 'lock' else 'c'*64
    try:
        if mode == 'valid':
            with open_holdout_depth(source,provider,artifacts.scratch,model_lock_sha=lock,
                    execution_profile=execution,check_active=active) as depth:
                expected,_ = normalize_depth(np.arange(20,dtype=np.float32).reshape(4,5),
                                             artifacts.rgb.shape[1],artifacts.rgb.shape[0])
                assert np.array_equal(depth.view(np.uint32),expected.view(np.uint32))
                assert not depth.flags.writeable
        else:
            with pytest.raises((ValueError,RuntimeError)):
                with open_holdout_depth(source,provider,artifacts.scratch,model_lock_sha=lock,
                        execution_profile=execution,check_active=active):
                    pytest.fail('invalid depth yielded')
        assert list(artifacts.scratch.directory.iterdir()) == []
        if mode in ('missing','lock','transparent'): assert events == []
        if mode in ('cancel','drift'): assert events[-1][0] == 'close'
    finally:
        artifacts.scratch.cleanup()


@pytest.mark.parametrize('size', [(True,2),(2,False),(0,2),(-1,2),(200000001,1),(2.0,2)])
def test_depth_canvas_refuses_before_native_resize(monkeypatch,size):
    called = []
    monkeypatch.setattr('photo_worker.operations.depth.prepare.cv2.resize',
                        lambda *args,**kwargs: called.append(True))
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        normalize_depth(np.ones((2,2),np.float32),*size)
    assert called == []


@pytest.mark.parametrize('value', [float('nan'),float('inf'),float('-inf')])
def test_invalid_native_resize_result_is_never_normalized(monkeypatch,value):
    monkeypatch.setattr('photo_worker.operations.depth.prepare.cv2.resize',
        lambda *args,**kwargs: np.full((3,4),value,np.float32))
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        normalize_depth(np.ones((2,2),np.float32),4,3)


def test_depth_model_output_rejects_before_array_conversion(monkeypatch):
    class HostileOutput:
        shape = (200000001, 2)

        def __array__(self, *_args, **_kwargs):
            pytest.fail('invalid model output was converted')

    resized = []
    monkeypatch.setattr('photo_worker.operations.depth.prepare.cv2.resize',
                        lambda *args, **kwargs: resized.append(True))
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        normalize_depth(HostileOutput(), 4, 3)
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        normalize_depth(np.ones((1, 1, 2), np.float32), 4, 3)
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        normalize_depth(np.ones((2, 2), np.complex64), 4, 3)
    assert resized == []
