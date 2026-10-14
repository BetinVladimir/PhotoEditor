"""Holdout background stages accept only the source alpha contracts."""

from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.contracts.style import Background
from photo_worker.operations.learning.background import render_holdout_background


def test_solid_holdout_refuses_unsupported_alpha_without_changing_output():
    source = SimpleNamespace(
        rgb=np.full((2, 2, 3), .4, dtype=np.float32),
        alpha=np.ones((2, 2), dtype=np.uint8),
        protected=np.zeros((2, 2), dtype=np.float32))
    target = source.rgb.copy()
    opacity = np.zeros((2, 2), dtype=np.float32)
    style = SimpleNamespace(extensions=SimpleNamespace(background=Background(
        mode='solid', asset_id=None, solid_rgb=[.2, .2, .2],
        blur_strength=0, protect_subject=True)))

    with pytest.raises(ValueError, match='HOLDOUT_BACKGROUND_ALPHA_INVALID'):
        render_holdout_background(
            source, target, opacity, style,
            subject=np.full((2, 2), .8, dtype=np.float32), confidence=.9)
    np.testing.assert_array_equal(target, source.rgb)
    np.testing.assert_array_equal(opacity, np.zeros((2, 2), dtype=np.float32))
