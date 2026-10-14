import numpy as np
import pytest
from photo_worker.algorithms.multi_proxy import linear_registration_proxy
from photo_worker.operations.optical.multi_prepare import prepare_automatic_planar_stack


def test_proxy_clips_only_detection_luma_and_excludes_transparent_boundaries():
    rgb = np.full((100, 120, 3), 4., np.float32); alpha = np.ones((100, 120), np.float32)
    alpha[:, :10] = 0
    proxy = linear_registration_proxy(rgb, alpha, luma=(.2126, .7152, .0722))
    assert np.all(proxy.pixels[:, 10:] == 255) and not proxy.pixels[:, :10].any()
    assert not proxy.mask[:, :41].any() and proxy.mask[50, 60] == 255
    assert np.all(rgb == 4) and not proxy.pixels.flags.writeable


def test_proxy_validates_pixels_outside_visible_coverage():
    rgb = np.ones((64, 64, 3), np.float32); rgb[0, 0, 0] = np.nan
    with pytest.raises(ValueError, match='MULTI_PROXY_INVALID'):
        linear_registration_proxy(rgb, np.zeros((64, 64), np.float32), luma=(.2126, .7152, .0722))


def test_automatic_stack_rejects_unregistered_blank_frames_before_scratch():
    class NoScratch:
        budget_bytes = 2**40
        def reserve(self, _size): pytest.fail('unexpected allocation')
    frame = np.zeros((100, 120, 3), np.float32); alpha = np.ones((100, 120), np.float32)
    with pytest.raises(ValueError, match='UNREGISTERED_FRAME'):
        with prepare_automatic_planar_stack([frame]*2, [alpha]*2, 0, NoScratch(),
                operation='panorama', max_pixels=12000, ram_max_bytes=2**40,
                registration_error_px=1., match_inlier_min=.8,
                motion_threshold=.5, luma=(.2126, .7152, .0722)):
            pytest.fail('unexpected result')
