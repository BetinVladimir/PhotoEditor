"""Scene parsing admits whole-frame conversion and bounded model logits."""

from types import SimpleNamespace
import numpy as np
import pytest

from photo_worker.operations.scene.parse import _required_scene_ram, _valid_scene_image, decode_logits


def test_scene_large_frame_exceeds_small_ram_profile():
    assert _required_scene_ram(10_000, 10_000) > 10 * 1024**3
    assert _required_scene_ram(1024, 1024) < 1024**3


def test_scene_working_color_is_required():
    image = SimpleNamespace(dtype='float32', color=SimpleNamespace(
        profile_id='work_rec2020_gamma24_v1', encoding='gamma24'),
        canvas=SimpleNamespace(width=4000, height=3000))
    assert _valid_scene_image(image)
    image.color.profile_id = 'linear_srgb_v1'
    assert not _valid_scene_image(image)


def test_scene_rejects_oversized_or_empty_logits():
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        decode_logits(np.zeros((150, 1, 1025), np.float32), 10, 10)
    with pytest.raises(ValueError, match='MODEL_OUTPUT_INVALID'):
        decode_logits(np.zeros((150, 0, 1), np.float32), 10, 10)
