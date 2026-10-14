"""V28 safe-delta exactness and generated-pixel semantics."""

import numpy as np
import pytest

from photo_worker.algorithms.safe_delta import compose_safe_delta


def test_safe_delta_preserves_outside_and_any_protected_support():
    source = np.full((2, 3, 3), .2, np.float32)
    proposal = np.full_like(source, .8)
    target = np.array([[1, .5, 0], [1, 1, 1]], np.float32)
    protected = np.array([[0, 0, 0], [.001, 1, 0]], np.float32)
    result, changed, generated = compose_safe_delta(source, proposal, target, protected, .5)
    assert result[0, 0, 0] == pytest.approx(.5)
    assert result[0, 1, 0] == pytest.approx(.35)
    for y, x in ((0, 2), (1, 0), (1, 1)):
        assert np.array_equal(result[y, x].view(np.uint32), source[y, x].view(np.uint32))
        assert changed[y, x] == 0 and generated[y, x] == 0
    assert changed[0, 0] == 1 and generated[0, 0] == 1


def test_safe_delta_zero_strength_and_numerical_guard():
    source = np.array([[[.2, .3, .4]]], np.float32)
    target = np.ones((1, 1), np.float32)
    zero = np.zeros((1, 1), np.float32)
    result, changed, generated = compose_safe_delta(source, source * 2, target, zero, 0)
    assert np.array_equal(result.view(np.uint32), source.view(np.uint32))
    assert not changed.any() and not generated.any()
    with pytest.raises(ValueError, match='SAFE_DELTA_INPUT_INVALID'):
        compose_safe_delta(source, source, np.array([[np.nan]], np.float32), zero, 1)
