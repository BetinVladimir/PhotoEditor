import numpy as np
import pytest

from photo_worker.operations.session.describe_features import frame_statistics, normalize_scene_embedding


def test_neutral_quantiles_and_no_clipping():
    rgb = np.full((32, 32, 3), .5, dtype=np.float64)
    stats = frame_statistics(rgb)
    assert stats.neutral_rgb == (.5, .5, .5)
    assert stats.luma_quantiles == (.5, .5, .5, .5, .5)
    assert stats.sharpness == 0
    assert stats.clipping_fraction == 0


def test_neutral_requires_1024_samples_and_excludes_clipping():
    rgb = np.full((32, 32, 3), .5, dtype=np.float64)
    rgb[0, 0] = [1, 1, 1]
    stats = frame_statistics(rgb)
    assert stats.neutral_rgb is None
    assert stats.clipping_fraction == pytest.approx(1 / 1024)


def test_embedding_requires_384_finite_nonzero_values():
    value = np.zeros(384)
    value[0] = 2
    assert normalize_scene_embedding(value)[0] == 1
    with pytest.raises(ValueError):
        normalize_scene_embedding(np.zeros(384))
    with pytest.raises(ValueError):
        normalize_scene_embedding(np.ones(383))
