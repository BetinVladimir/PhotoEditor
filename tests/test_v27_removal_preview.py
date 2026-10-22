import numpy as np
import pytest
from test_v27_removal_preview_binding import removal_fixture
from photo_worker.operations.semantic.preview_removal_loader import RemovalPreviewArrays
from photo_worker.operations.semantic.preview_removal import compute_removal_preview
from photo_worker.runtime.executor import OperationFailure


@pytest.fixture(autouse=True, params=['remove_object','background_cleanup'])
def inpaint_preview_kind(request, monkeypatch):
    if request.param == 'background_cleanup':
        from hashlib import sha256
        from photo_worker.contracts.base import canonical_bytes, parse_json
        from photo_worker.operations.semantic.preview_background_cleanup import compute_background_cleanup_preview
        original_fixture = removal_fixture
        def background_fixture():
            params, candidate, raw = original_fixture()
            params['kind'] = 'background_cleanup'
            intent = parse_json(raw)
            intent['kind'] = 'background_cleanup'
            raw = canonical_bytes(intent)
            params['intent']['sha256'] = sha256(raw).hexdigest()
            return params, candidate, raw
        monkeypatch.setattr(__import__(__name__, fromlist=['removal_fixture']), 'removal_fixture', background_fixture)
        monkeypatch.setattr(__import__(__name__, fromlist=['compute_removal_preview']), 'compute_removal_preview', compute_background_cleanup_preview)


class Engine:
    def __init__(self):
        self.calls = 0

    def infer(self, image, mask):
        self.calls += 1
        return np.clip(image + .02, 0, 1)


def arrays():
    rgb = np.full((16, 16, 3), .4, np.float32)
    mask = np.zeros((16, 16), np.float32); mask[4:12, 4:12] = 1
    protected = np.zeros_like(mask); protected[7:9, 7:9] = 1
    return RemovalPreviewArrays(rgb, mask, protected, 64*1048576, 0)


def compute(value, engine, admission=lambda _: True, budget=1024**3, strength=1.):
    params, _, _ = removal_fixture()
    return compute_removal_preview(value, params, engine,
        verify_admission=admission, max_bytes=budget, check_active=lambda: None, strength=strength)


def test_private_candidate_preserves_source_and_protected_bits():
    value = arrays(); before = value.original.copy(); engine = Engine()
    result = compute(value, engine)
    assert engine.calls == 1 and result.preservation.changed_pixels > 0
    keep = (value.mask == 0) | (value.protected > 0)
    np.testing.assert_array_equal(result.candidate[keep].view(np.uint32), before[keep].view(np.uint32))
    np.testing.assert_array_equal(value.original.view(np.uint32), before.view(np.uint32))
    assert not np.shares_memory(result.candidate, value.original)


def test_resource_denial_precedes_model_call():
    engine = Engine()
    with pytest.raises(OperationFailure):
        compute(arrays(), engine, budget=64*1048576)
    assert engine.calls == 0


def test_admission_loss_after_inference_does_not_return_candidate():
    engine = Engine()
    with pytest.raises(ValueError, match='ADMISSION_DENIED'):
        compute(arrays(), engine, admission=lambda _: engine.calls == 0)
    assert engine.calls == 1


def test_hdr_is_rejected_without_clipping_or_model_call():
    value = arrays(); value.original[0, 0, 0] = 2
    engine = Engine()
    with pytest.raises(ValueError, match='MODEL_ADAPTER_INPUT_INVALID'):
        compute(value, engine)
    assert engine.calls == 0 and value.original[0, 0, 0] == 2


def test_any_positive_protection_excludes_pixel():
    value = arrays(); value.protected[5, 5] = .01
    result = compute(value, Engine())
    np.testing.assert_array_equal(result.candidate[5, 5].view(np.uint32), value.original[5, 5].view(np.uint32))


def test_zero_strength_is_exact_source_copy():
    value = arrays()
    result = compute(value, Engine(), strength=0.)
    np.testing.assert_array_equal(result.candidate.view(np.uint32), value.original.view(np.uint32))
    assert result.preservation.changed_pixels == 0


def test_half_strength_halves_working_space_correction():
    value = arrays()
    full = compute(value, Engine(), strength=1.)
    half = compute(value, Engine(), strength=.5)
    np.testing.assert_allclose(half.candidate, value.original +
        (full.candidate - value.original) * .5, atol=6e-8, rtol=0)


@pytest.mark.parametrize('strength', [True, -.1, 1.1, float('nan')])
def test_invalid_strength_denied_before_model(strength):
    engine = Engine()
    with pytest.raises(ValueError, match='STRENGTH_INVALID'):
        compute(arrays(), engine, strength=strength)
    assert engine.calls == 0


def test_large_roi_runs_global_then_local_and_preserves_excluded_pixels():
    rgb=np.full((8,1025,3),.4,np.float32)
    mask=np.ones((8,1025),np.float32)
    protection=np.zeros_like(mask);protection[3,500]=1
    value=RemovalPreviewArrays(rgb,mask,protection,64*1048576,0)
    before=rgb.copy();engine=Engine()
    result=compute(value,engine)
    assert result.global_pass is True and engine.calls==2
    assert result.roi==(0,0,1025,8)
    np.testing.assert_array_equal(result.candidate[3,500].view(np.uint32),before[3,500].view(np.uint32))
    np.testing.assert_array_equal(rgb.view(np.uint32),before.view(np.uint32))


def test_cancellation_after_global_pass_prevents_local_inference():
    value=RemovalPreviewArrays(np.full((8,1025,3),.4,np.float32),
        np.ones((8,1025),np.float32),np.zeros((8,1025),np.float32),64*1048576,0)
    engine=Engine();params,_,_=removal_fixture()
    def active():
        if engine.calls:raise RuntimeError('cancelled after global')
    with pytest.raises(RuntimeError,match='cancelled after global'):
        compute_removal_preview(value,params,engine,verify_admission=lambda _:True,
            max_bytes=1024**3,check_active=active,strength=1.)
    assert engine.calls==1


@pytest.mark.parametrize('blocked',[False,True])
def test_empty_effective_mask_does_not_invoke_model(blocked):
    value=arrays()
    if blocked:value.protected[:]=1
    else:value.mask[:]=0
    engine=Engine()
    with pytest.raises(ValueError,match='INPAINT_MASK_EMPTY'):
        compute(value,engine)
    assert engine.calls==0
