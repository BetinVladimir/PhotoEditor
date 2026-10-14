"""Quality and protection oracle for V23 holdout asset composition."""

from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.learning.background_asset import render_holdout_asset_background
from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
from photo_worker.v2.render_background_decode import NormalizedBackground


def test_holdout_asset_composes_in_linear_light_and_restores_protected_pixel():
    work_sha = 'c' * 64
    style_ref = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    pin = FrozenBackgroundAsset(
        schema_version=1, portable_asset_id='studio', style_version_id=uuid4(),
        style=style_ref, source=ArtifactRef(artifact_id=uuid4(), sha256='b' * 64),
        expected_bytes=5, media_type='image/png', suffix='.png',
        assume_profile=None, work_icc_sha=work_sha)
    style = StyleV2.model_construct(extensions=SimpleNamespace(
        background=SimpleNamespace(mode='asset', asset_id='studio')))
    source_rgb = np.full((2, 2, 3), .4, dtype=np.float32)
    protected = np.zeros((2, 2), dtype=np.float32)
    protected[0, 1] = 1
    source = SimpleNamespace(rgb=source_rgb, alpha=np.array(
        [[32768, 32768], [0, 65535]], dtype=np.uint16), protected=protected)
    target = source_rgb.copy()
    alpha_out = np.zeros((2, 2), dtype=np.float32)
    normalized = NormalizedBackground(
        SimpleNamespace(height=2, width=2, profile_sha256=work_sha),
        np.full((2, 2, 3), .2, dtype=np.float32))
    subject = np.full((2, 2), .8, dtype=np.float32)

    render_holdout_asset_background(
        source, target, alpha_out, style, style_ref, pin, normalized,
        subject=subject, confidence=.9, work_icc_sha=work_sha)

    coverage = .8 * (32768 / 65535)
    expected = (.4 ** 2.4 * coverage + .2 ** 2.4 * (1 - coverage)) ** (1 / 2.4)
    np.testing.assert_allclose(target[0, 0], expected, atol=1e-6)
    np.testing.assert_array_equal(target[0, 1], source_rgb[0, 1])
    np.testing.assert_allclose(target[1, 0], .2, atol=1e-6)
    np.testing.assert_allclose(alpha_out, [[1, 32768 / 65535], [1, 1]], atol=1e-6)
    np.testing.assert_array_equal(source.rgb, np.full((2, 2, 3), .4, dtype=np.float32))

    before = target.copy()
    with pytest.raises(ValueError, match='HOLDOUT_BACKGROUND_ASSET_BINDING_MISMATCH'):
        render_holdout_asset_background(
            source, target, alpha_out, style, style_ref, pin, normalized,
            subject=subject, confidence=.9, work_icc_sha='d' * 64)
    np.testing.assert_array_equal(target, before)
