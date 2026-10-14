"""V28 reflection proposal overlap, shape, and fail-closed guards."""

import numpy as np
import pytest

from photo_worker.operations.image.reflection_tiles import reflection_proposal
from photo_worker.runtime.executor import OperationFailure, TaskCancelled


class ConstantEngine:
    size = 256

    def __init__(self):
        self.calls = 0

    def infer(self, tile, *, check_active):
        self.calls += 1
        assert tile.shape == (256, 256, 3)
        assert check_active() is not False
        return np.full_like(tile, .4)


def test_reflection_tiling_restores_shape_and_normalizes_overlap():
    engine = ConstantEngine()
    source = np.full((301, 421, 3), .2, np.float32)
    result = reflection_proposal(source, engine)
    assert result.shape == source.shape and result.dtype == np.float32
    assert np.allclose(result, .4)
    assert engine.calls > 1


def test_reflection_tiling_reflect_pads_small_image():
    engine = ConstantEngine()
    result = reflection_proposal(np.ones((7, 9, 3), np.float32), engine)
    assert result.shape == (7, 9, 3) and np.allclose(result, .4)
    assert engine.calls == 1


def test_reflection_tiling_rejects_extreme_aspect_before_padding_or_inference():
    engine = ConstantEngine()
    source = np.broadcast_to(np.zeros((1, 1, 3), np.float32),
                             (1, 1_000_000, 3))
    with pytest.raises(OperationFailure, match='RESOURCE_OOM'):
        reflection_proposal(source, engine)
    assert engine.calls == 0


def test_reflection_tiling_rejects_bad_engine_output_and_cancellation():
    class InvalidEngine(ConstantEngine):
        def infer(self, tile, *, check_active):
            return np.full_like(tile, np.nan)

    source = np.zeros((256, 256, 3), np.float32)
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        reflection_proposal(source, InvalidEngine())
    with pytest.raises(TaskCancelled):
        reflection_proposal(source, ConstantEngine(), check_active=lambda: False)
