from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.optical.panorama_prepare import prepare_panorama_blend, panorama_blend_resources
from photo_worker.runtime.scratch import ScratchSpace


def test_panorama_preparation_persists_and_cleans_owned_outputs(tmp_path):
    frames = [np.full((9, 11, 3), 2., np.float32), np.full((9, 11, 3), 6., np.float32)]
    alphas = [np.ones((9, 11), np.float32)]*2; coverage = [np.ones((9, 11), bool)]*2
    resources = panorama_blend_resources((9, 11), 2, tile=3)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, resources['scratch_bytes'], min_free_bytes=0) as scratch:
        with prepare_panorama_blend(frames, alphas, coverage, scratch, motion_threshold=.5,
                ram_max_bytes=resources['ram_bytes'], tile=3) as files:
            np.testing.assert_array_equal(np.load(files.rgb), np.full((9, 11, 3), 4., np.float32))
            assert np.load(files.motion).all() and files.motion_fraction == 1
            files.reservation.validate(); paths = [files.rgb, files.alpha, files.motion]
        assert all(not path.exists() for path in paths)


def test_panorama_ram_rejection_precedes_scratch():
    class NoScratch:
        def reserve(self, _size): pytest.fail('unexpected allocation')
    frame = np.zeros((9, 11, 3), np.float32)
    with pytest.raises(ValueError, match='PANORAMA_RAM_EXCEEDED'):
        with prepare_panorama_blend([frame, frame], [np.ones((9,11),np.float32)]*2, [np.ones((9,11),bool)]*2, NoScratch(), motion_threshold=.5, ram_max_bytes=1):
            pytest.fail('unexpected result')


def test_invalid_panorama_frame_rejected_before_scratch():
    frame = np.ones((7, 9, 3), np.float32)
    alpha = np.ones((7, 9), np.float32)
    coverage = np.ones((7, 9), bool)
    class NoScratch:
        def reserve(self, _size): pytest.fail('invalid input reserved scratch')
    with pytest.raises(ValueError, match='PANORAMA_BLEND_INVALID'):
        with prepare_panorama_blend([frame, frame.astype(np.float64)], [alpha]*2, [coverage]*2,
                NoScratch(), motion_threshold=.5, ram_max_bytes=1024**3):
            pytest.fail('unexpected result')


def test_panorama_resources_account_stack_and_row_cache():
    small = panorama_blend_resources((10, 20), 2, tile=64)
    many = panorama_blend_resources((10, 20), 3, tile=64)
    wide = panorama_blend_resources((1, 200), 2, tile=64)
    assert many['ram_bytes'] > small['ram_bytes']
    assert wide['ram_bytes'] > small['ram_bytes']
    assert small['vram_bytes'] == 0
