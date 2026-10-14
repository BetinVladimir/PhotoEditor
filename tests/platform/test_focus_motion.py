import numpy as np
import pytest
from photo_worker.algorithms.focus_motion import detect_focus_motion, apply_focus_motion_policy


def test_reference_motion_dilates_exact_disk_and_ignores_nonoverlap():
    ref = np.zeros((11, 11, 3), np.float32); moved = ref.copy(); moved[5, 5] = 1
    opacity = np.ones((11, 11), np.float32); coverage = np.ones((11, 11), bool)
    motion = np.empty((11, 11), bool)
    fraction = detect_focus_motion([ref, moved], [opacity, opacity], [coverage, coverage], 0, motion,
                                   motion_threshold=.5, tile=2)
    yy, xx = np.mgrid[:11, :11]; expected = (xx-5)**2+(yy-5)**2 <= 9
    np.testing.assert_array_equal(motion, expected)
    assert fraction == np.count_nonzero(expected)/121
    coverage[5, 5] = False
    assert detect_focus_motion([ref, moved], [opacity, opacity], [coverage, coverage], 0, motion,
                               motion_threshold=.5) == 0


def test_reject_does_not_modify_output_and_review_copies_exact_reference():
    ref = np.full((7, 9, 3), 4., np.float32); ref_alpha = np.full((7, 9), .25, np.float32)
    rgb = np.full((7, 9, 3), -2., np.float64); alpha = np.ones((7, 9), np.float32)
    motion = np.zeros((7, 9), bool); motion[2:5, 3:6] = True
    original = rgb.copy()
    with pytest.raises(ValueError, match='MOTION_DETECTED'):
        apply_focus_motion_policy(rgb, alpha, ref, ref_alpha, motion, policy='reject', tile=2)
    np.testing.assert_array_equal(rgb, original); assert np.all(alpha == 1)
    fraction = apply_focus_motion_policy(rgb, alpha, ref, ref_alpha, motion, policy='review', tile=3)
    assert np.all(rgb[motion] == 4) and np.all(alpha[motion] == .25)
    assert np.all(rgb[~motion] == -2) and np.all(alpha[~motion] == 1)
    assert fraction == 9/63


def test_motion_rejects_output_alias():
    rgb = np.zeros((3, 3, 3), np.float32); alpha = np.ones((3, 3), np.float32); motion = np.zeros((3, 3), bool)
    with pytest.raises(ValueError, match='FOCUS_MOTION_INVALID'):
        apply_focus_motion_policy(rgb, alpha, rgb, alpha, motion, policy='review')
