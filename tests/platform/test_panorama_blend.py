import numpy as np
import pytest
from photo_worker.algorithms.panorama_blend import blend_panorama


def blend(frames, alphas=None, coverages=None, threshold=.5, tile=3):
    shape = frames[0].shape[:2]
    if alphas is None: alphas = [np.ones(shape, np.float32) for _ in frames]
    if coverages is None: coverages = [np.ones(shape, bool) for _ in frames]
    rgb = np.empty((*shape, 3), np.float32); alpha = np.empty(shape, np.float32); motion = np.empty(shape, bool)
    fraction = blend_panorama(frames, alphas, coverages, rgb, alpha, motion,
                             motion_threshold=threshold, tile=tile)
    return rgb, alpha, motion, fraction


def test_equal_coverage_preserves_hdr_average_and_tile_invariance():
    frames = [np.full((9, 11, 3), -2, np.float32), np.full((9, 11, 3), 6, np.float32)]
    for tile in [1, 3, 11]:
        rgb, alpha, motion, fraction = blend(frames, tile=tile)
        np.testing.assert_array_equal(rgb, np.full_like(rgb, 2))
        assert np.all(alpha == 1) and motion.all() and fraction == 1
    assert np.all(frames[0] == -2) and np.all(frames[1] == 6)


def test_zero_coverage_and_transparent_color_cannot_contribute():
    frames = [np.full((5, 5, 3), 4, np.float32), np.full((5, 5, 3), 1000, np.float32)]
    alphas = [np.ones((5, 5), np.float32), np.zeros((5, 5), np.float32)]
    rgb, alpha, motion, fraction = blend(frames, alphas)
    assert np.all(rgb == 4) and np.all(alpha == 1) and not motion.any() and fraction == 0
    rgb, alpha, motion, _ = blend(frames, coverages=[np.zeros((5, 5), bool)]*2)
    assert not rgb.any() and not alpha.any() and not motion.any()


def test_motion_uses_exact_radius_three_disk_without_tile_seams():
    a = np.zeros((13, 13, 3), np.float32); b = a.copy(); b[6, 6] = 1
    rgb, alpha, motion, fraction = blend([a, b], tile=2)
    yy, xx = np.mgrid[:13, :13]
    expected = (xx-6)**2+(yy-6)**2 <= 9
    np.testing.assert_array_equal(motion, expected)
    assert fraction == np.count_nonzero(expected)/169


def test_output_alias_rejected():
    rgb = np.ones((4, 4, 3), np.float32); alpha = np.ones((4, 4), np.float32); coverage = np.ones((4, 4), bool)
    with pytest.raises(ValueError, match='PANORAMA_BLEND_ALIAS'):
        blend_panorama([rgb, rgb], [alpha, alpha], [coverage, coverage], rgb,
                       np.empty_like(alpha), np.empty_like(coverage), motion_threshold=.1)
