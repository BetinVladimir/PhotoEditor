import numpy as np
import pytest
from photo_worker.operations.semantic.candidate_inpaint import (
    infer_inpaint_candidate, inpaint_mask_roi,
)
from photo_worker.runtime.executor import OperationFailure


class Engine:
    def __init__(self, transform=lambda image: image):
        self.calls = []
        self.transform = transform

    def infer(self, image, mask):
        self.calls.append((image.copy(), mask.copy()))
        return self.transform(image)


def test_padding_and_crop_preserve_source_and_mask():
    rgb = np.arange(5 * 7 * 3, dtype=np.float32).reshape(5, 7, 3) / 105
    mask = np.zeros((5, 7), dtype=bool)
    mask[2, 3] = True
    engine = Engine()
    result = infer_inpaint_candidate(engine, rgb, mask, 64)
    np.testing.assert_array_equal(result, rgb)
    image, padded_mask = engine.calls[0]
    np.testing.assert_array_equal(image, np.pad(rgb, ((0, 3), (0, 1), (0, 0)), mode="reflect"))
    np.testing.assert_array_equal(padded_mask[:5, :7], mask.astype(np.float32))
    assert not padded_mask[5:].any() and not padded_mask[:, 7:].any()
    assert image.shape == (8, 8, 3)


@pytest.mark.parametrize("budget", [63, 0, -1, True, 64.0])
def test_budget_denies_before_model_call(budget):
    engine = Engine()
    with pytest.raises(OperationFailure):
        infer_inpaint_candidate(engine, np.zeros((5, 7, 3), np.float32), np.zeros((5, 7), bool), budget)
    assert engine.calls == []


@pytest.mark.parametrize("transform", [
    lambda image: image[:, :-1],
    lambda image: np.full_like(image, np.nan),
    lambda image: np.full_like(image, -0.1),
    lambda image: np.full_like(image, 1.1),
])
def test_rejects_invalid_model_output(transform):
    with pytest.raises(ValueError, match="MODEL_OUTPUT_INVALID"):
        infer_inpaint_candidate(Engine(transform), np.zeros((8, 8, 3), np.float32), np.ones((8, 8), bool), 64)


def test_roi_context_clips_to_canvas():
    mask = np.zeros((10, 12), bool)
    mask[1:3, 8:11] = True
    assert inpaint_mask_roi(mask, 2) == (6, 0, 12, 5)
    assert inpaint_mask_roi(mask, 0) == (8, 1, 11, 3)


def test_empty_mask_has_no_candidate_roi():
    with pytest.raises(ValueError, match="INPAINT_MASK_EMPTY"):
        inpaint_mask_roi(np.zeros((3, 4), bool), 2)


@pytest.mark.parametrize("mask,context", [
    (np.ones((3, 4), np.float32), 2),
    (np.ones((3, 4), bool), True),
    (np.ones((3, 4), bool), -1),
    (np.ones((0, 4), bool), 2),
])
def test_invalid_roi_inputs(mask, context):
    with pytest.raises(ValueError, match="INPAINT_MASK_INVALID"):
        inpaint_mask_roi(mask, context)


from photo_worker.operations.semantic.candidate_inpaint import compute_inpaint_region


def test_region_weights_exclude_protection_and_outside_mask():
    source = np.full((8, 8, 3), .4, np.float32)
    mask = np.zeros((8, 8), bool); mask[2:6, 2:6] = True
    protected = np.zeros_like(mask); protected[3, 3] = True
    restored, weight, global_pass = compute_inpaint_region(
        Engine(), source, mask & ~protected, protected, (0, 0, 8, 8),
        feather_pixels=2, max_padded_pixels=64, check_active=lambda: None)
    assert restored.shape == source.shape and np.isfinite(restored).all()
    assert weight[3, 3] == 0 and not weight[~mask].any()
    assert weight[2, 2] > 0 and global_pass is False


def test_region_cancellation_after_model_produces_no_result():
    source = np.full((8, 8, 3), .4, np.float32)
    engine = Engine()
    def active():
        if engine.calls:
            raise RuntimeError("cancelled")
    with pytest.raises(RuntimeError, match="cancelled"):
        compute_inpaint_region(engine, source, np.ones((8, 8), bool),
            np.zeros((8, 8), bool), (0, 0, 8, 8), feather_pixels=2,
            max_padded_pixels=64, check_active=active)
    assert len(engine.calls) == 1


def test_region_padding_budget_denies_before_engine():
    engine = Engine(); source = np.full((9, 9, 3), .4, np.float32)
    with pytest.raises(OperationFailure):
        compute_inpaint_region(engine, source, np.ones((9, 9), bool),
            np.zeros((9, 9), bool), (0, 0, 9, 9), feather_pixels=2,
            max_padded_pixels=81, check_active=lambda: None)
    assert not engine.calls
