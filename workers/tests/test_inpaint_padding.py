"""The inpaint RAM reservation covers the model's padded ROI, not just source area."""

import numpy as np
import pytest

from photo_worker.operations.image.inpaint import (
    _blend_selected, _infer, _mask_roi, _required_frame_ram,
    _roi_pixel_budget,
)
from photo_worker.runtime.executor import OperationFailure


class EchoEngine:
    def __init__(self):
        self.calls = 0

    def infer(self, image, mask):
        self.calls += 1
        return image.copy()


def test_inpaint_refuses_skinny_roi_before_padding_or_model_io():
    engine = EchoEngine()
    rgb = np.zeros((1, 1000, 3), np.float32)
    mask = np.ones((1, 1000), np.float32)
    with pytest.raises(OperationFailure, match='RESOURCE_OOM'):
        _infer(engine, rgb, mask, 1000)
    assert engine.calls == 0


def test_inpaint_keeps_normal_padding_under_reserved_limit():
    engine = EchoEngine()
    rgb = np.full((9, 9, 3), .25, np.float32)
    mask = np.ones((9, 9), np.float32)
    result = _infer(engine, rgb, mask, 256)
    assert result.shape == rgb.shape and np.array_equal(result, rgb)
    assert engine.calls == 1


def test_inpaint_roi_uses_compact_axis_projections(monkeypatch):
    mask = np.zeros((20, 30), bool)
    mask[3, 4] = True
    mask[15, 21] = True
    monkeypatch.setattr(np, 'nonzero', lambda *_: pytest.fail('per-pixel coordinates'))
    assert _mask_roi(mask, 2) == (2, 1, 24, 18)
    assert _mask_roi(np.ones((20, 30), bool), 2) == (0, 0, 30, 20)
    with pytest.raises(ValueError, match='INPAINT_MASK_EMPTY'):
        _mask_roi(np.zeros((20, 30), bool), 2)


def test_inpaint_full_frame_ram_accounts_for_mask_materialization():
    assert _required_frame_ram(20_000, 10_000) > 4 * 1024**3
    assert _required_frame_ram(1024, 1024) < 1024**3


def test_inpaint_roi_budget_reserves_full_frame_and_delta_temporaries():
    ram = 4 * 1024**3
    frame = _required_frame_ram(4000, 4000)
    assert _roi_pixel_budget(ram, 4000, 4000) == min(
        32_000_000, (ram - frame) // 192)
    assert _roi_pixel_budget(frame, 4000, 4000) == 0
    assert _roi_pixel_budget(32 * 1024**3, 4000, 4000) == 32_000_000


def test_inpaint_blend_preserves_zero_weight_float_bits():
    original = np.zeros((2, 3, 3), np.float32)
    original[0, 0, 0] = np.float32(-0.0)
    original[1, 2, 2] = np.float32(.375)
    restored = np.ones_like(original)
    weight = np.zeros((2, 3), np.float32)
    weight[0, 1] = 1
    output = original.copy()
    changed = _blend_selected(output, original, restored, weight,
                              0, 0, lambda: None)
    frozen = weight == 0
    assert np.array_equal(output[frozen].view(np.uint32),
                          original[frozen].view(np.uint32))
    assert np.array_equal(output[0, 1], np.ones(3, np.float32))
    assert changed == 1


def test_inpaint_reports_zero_changed_when_model_keeps_selected_rgb():
    original = np.full((2, 2, 3), .25, np.float32)
    output = original.copy()
    weight = np.ones((2, 2), np.float32)
    assert _blend_selected(output, original, original.copy(), weight,
                           0, 0, lambda: None) == 0
    assert np.array_equal(output.view(np.uint32), original.view(np.uint32))


def test_inpaint_checks_cancellation_after_last_blend_tile():
    original = np.zeros((1, 1, 3), np.float32)
    calls = 0

    def active():
        nonlocal calls
        calls += 1
        if calls == 2:
            raise RuntimeError('lease lost before stage')

    with pytest.raises(RuntimeError, match='lease lost before stage'):
        _blend_selected(original.copy(), original, np.ones_like(original),
                        np.ones((1, 1), np.float32), 0, 0, active)
    assert calls == 2
