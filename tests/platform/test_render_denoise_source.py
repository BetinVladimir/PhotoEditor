"""Exact source identity with bounded denoise validation temporaries."""
import numpy as np
import pytest
from photo_worker.v2.render_denoise import _same_denoise_pixels


@pytest.mark.parametrize('channels', [None, 3])
def test_denoise_source_tiles_wide_canvas_and_preserves_float_bits(monkeypatch, channels):
    shape = (3, 4099) if channels is None else (3, 4099, channels)
    expected = np.zeros(shape, dtype=np.float32)
    actual = expected.copy()
    compared = []
    original = np.array_equal
    def equal(left, right):
        compared.append(left.shape)
        assert left.shape[0] <= 256 and left.shape[1] <= 256
        return original(left, right)
    monkeypatch.setattr(np, 'array_equal', equal)
    _same_denoise_pixels(actual, expected, lambda: None)
    assert len(compared) == 17 and compared[-1][1] == 3
    actual[2, -1] = -0.0
    with pytest.raises(ValueError, match='CURRENT_PIXELS_CHANGED'):
        _same_denoise_pixels(actual, expected, lambda: None)


def test_denoise_source_checks_cancellation_each_tile():
    expected = np.zeros((1, 1024, 3), dtype=np.float32)
    calls = []
    def active():
        calls.append(None)
        if len(calls) == 2:
            raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError):
        _same_denoise_pixels(expected.copy(), expected, active)
    assert len(calls) == 2


@pytest.mark.parametrize('actual', [np.zeros((1,2),dtype=np.float64),
                                  np.zeros((2,2),dtype=np.float32), object()])
def test_denoise_source_rejects_invalid_array(actual):
    with pytest.raises(ValueError, match='CURRENT_PIXELS_CHANGED'):
        _same_denoise_pixels(actual, np.zeros((1,2),dtype=np.float32),lambda:None)


def test_denoise_factory_requires_matching_backend_and_model_lock():
    from types import SimpleNamespace
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.restoration import DenoiseParams
    from photo_worker.runtime.executor import OperationFailure
    from photo_worker.v2.render_denoise import PinnedDenoiseProviderFactory
    validated=[]
    factory=PinnedDenoiseProviderFactory(SimpleNamespace(lock=SimpleNamespace(lock_id='a'*64)),
        backend='cuda',provider_peak=dict(ram_bytes=1,scratch_bytes=2,vram_bytes=3,output_bytes=4),
        runtime_sha256='b'*64,verify_runtime=lambda:None,
        validate_binding=lambda params,state:validated.append(state))
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        factory(object(),object(),{'backend':'cpu'},lambda:None)
    provider,=factory(object(),object(),{'backend':'cuda'},lambda:None)
    params=DenoiseParams(image=ArtifactRef(artifact_id=uuid4(),sha256='c'*64),
        strength=.5,model_lock_id='d'*64)
    with pytest.raises(OperationFailure,match='MODEL_LOCK_MISMATCH'):
        provider.operator.validate_binding(params,'source')
    assert validated==[]
    provider.operator.validate_binding(params.model_copy(update={'model_lock_id':'a'*64}),'source')
    assert validated==['source']


@pytest.mark.parametrize('change',['source','strength','off'])
def test_authored_denoise_rejects_style_or_source_substitution(change):
    from types import SimpleNamespace
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.style import Restoration
    from photo_worker.contracts.restoration import DenoiseParams
    from photo_worker.v2.render_denoise import prepare_authored_denoise
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=DenoiseParams(image=source,strength=.7,model_lock_id='b'*64)
    authored=Restoration(denoise='scunet_color_real_psnr',denoise_strength=.7,
        upscale='off',sensor_dust='off')
    if change=='source':source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    if change=='strength':authored.denoise_strength=.5
    if change=='off':authored.denoise='off'
    artifacts=SimpleNamespace(contract=lambda *args:params)
    with pytest.raises(ValueError,match='AUTHORED_BINDING_INVALID'):
        with prepare_authored_denoise(artifacts,source,authored,
                ArtifactRef(artifact_id=uuid4(),sha256='c'*64),object(),object(),
                backend='cuda',execution_profile={},provider_peak={},verify_runtime=lambda:None):
            pytest.fail('Unauthored model execution')
