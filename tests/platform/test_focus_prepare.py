from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.optical.focus_prepare import prepare_focus_stack, focus_scratch_bytes, focus_resources
from photo_worker.runtime.scratch import ScratchSpace, DiskQuota


def inputs():
    rgb = np.full((9, 11, 3), 4., np.float32)
    alpha = np.ones((9, 11), np.float32); coverage = np.ones((9, 11), bool)
    return [rgb, rgb.copy()], [alpha, alpha.copy()], [coverage, coverage.copy()]


def test_focus_files_are_reserved_closed_and_removed_after_use(tmp_path):
    frames, alphas, coverages = inputs(); budget = focus_scratch_bytes((9, 11), 2)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, budget, min_free_bytes=0) as scratch:
        with prepare_focus_stack(frames, alphas, coverages, 0, scratch,
                luma=(.2126, .7152, .0722), motion_threshold=.5, motion_policy='review', ram_max_bytes=1024**3, tile=3) as files:
            colors = np.load(files.rgb); alpha = np.load(files.alpha)
            np.testing.assert_allclose(colors, 4, atol=1e-6)
            np.testing.assert_array_equal(alpha, alphas[0])
            assert files.motion_fraction == 0 and not np.load(files.motion).any()
            files.reservation.validate(); paths = [files.rgb, files.alpha, files.motion]
        assert all(not p.exists() for p in paths)


def test_scratch_limit_rejects_before_processing(tmp_path):
    frames, alphas, coverages = inputs()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 100, min_free_bytes=0) as scratch:
        with pytest.raises(DiskQuota):
            with prepare_focus_stack(frames, alphas, coverages, 0, scratch,
                    luma=(.2126, .7152, .0722), motion_threshold=.5, motion_policy='review', ram_max_bytes=1024**3):
                pytest.fail('unexpected result')


def test_motion_reject_never_yields_result(tmp_path):
    frames, alphas, coverages = inputs(); frames[1][4, 5] = 8
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, focus_scratch_bytes((9, 11), 2), min_free_bytes=0) as scratch:
        with pytest.raises(ValueError, match='MOTION_DETECTED'):
            with prepare_focus_stack(frames, alphas, coverages, 0, scratch,
                    luma=(.2126, .7152, .0722), motion_threshold=.5, motion_policy='reject', ram_max_bytes=1024**3):
                pytest.fail('unexpected result')


def test_ram_admission_precedes_scratch_reservation():
    frames, alphas, coverages = inputs()
    class NoScratch:
        def reserve(self, _size):
            pytest.fail('RAM rejection must precede disk allocation')
    required = focus_resources((9, 11), 2)
    with pytest.raises(ValueError, match='FOCUS_RAM_EXCEEDED'):
        with prepare_focus_stack(frames, alphas, coverages, 0, NoScratch(),
                luma=(.2126, .7152, .0722), motion_threshold=.5,
                motion_policy='review', ram_max_bytes=required['ram_bytes']-1):
            pytest.fail('unexpected result')


def test_invalid_focus_stack_shape_rejected_before_scratch():
    frames, alphas, coverages = inputs()
    class NoScratch:
        def reserve(self, _size): pytest.fail('invalid input reserved scratch')
    coverages[1] = np.ones((8, 11), bool)
    with pytest.raises(ValueError, match='FOCUS_INPUT_INVALID'):
        with prepare_focus_stack(frames, alphas, coverages, 0, NoScratch(),
                luma=(.2126, .7152, .0722), motion_threshold=.5,
                motion_policy='review', ram_max_bytes=1024**3):
            pytest.fail('unexpected result')


def test_focus_resource_budget_grows_with_stack_and_tile():
    small = focus_resources((65, 99), 2, tile=64)
    large = focus_resources((65, 99), 3, tile=64)
    tiled = focus_resources((65, 99), 2, tile=256)
    assert large['ram_bytes'] > small['ram_bytes']
    assert large['scratch_bytes'] > small['scratch_bytes']
    assert tiled['ram_bytes'] > small['ram_bytes']
    assert small['vram_bytes'] == 0
    assert small['scratch_bytes'] == focus_scratch_bytes((65, 99), 2)


def test_focus_tiles_preserve_reference_outside_coverage(tmp_path):
    yy, xx = np.mgrid[:17, :19]
    reference = np.stack((xx / 19, yy / 17, (xx + yy) / 36), axis=-1).astype(np.float32)
    alternate = reference.copy()
    alternate[4:14, 5:16] += .25
    frames = [reference, alternate]
    alphas = [np.ones((17, 19), np.float32) for _ in frames]
    coverages = [np.ones((17, 19), bool) for _ in frames]
    coverages[0][0, 0] = False
    coverages[1][0, 0] = False
    alphas[0][0, 0] = .375

    def prepare(tile):
        budget = focus_scratch_bytes((17, 19), 2)
        ram = focus_resources((17, 19), 2, tile=tile)['ram_bytes']
        with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, budget, min_free_bytes=0) as scratch:
            with prepare_focus_stack(frames, alphas, coverages, 0, scratch,
                    luma=(.2126, .7152, .0722), motion_threshold=1e9,
                    motion_policy='review', ram_max_bytes=ram, tile=tile) as files:
                return np.load(files.rgb).copy(), np.load(files.alpha).copy()

    fine_rgb, fine_alpha = prepare(3)
    coarse_rgb, coarse_alpha = prepare(7)
    np.testing.assert_allclose(fine_rgb, coarse_rgb, rtol=0, atol=1e-6)
    np.testing.assert_array_equal(fine_alpha, coarse_alpha)
    np.testing.assert_array_equal(fine_rgb[0, 0], reference[0, 0])
    assert fine_alpha[0, 0] == alphas[0][0, 0]


def test_transparent_alternate_cannot_contaminate_visible_focus_output(tmp_path):
    reference = np.full((9, 11, 3), .25, np.float32)
    alternate = np.full((9, 11, 3), 8., np.float32)
    alpha = [np.ones((9, 11), np.float32), np.zeros((9, 11), np.float32)]
    coverage = [np.ones((9, 11), bool), np.ones((9, 11), bool)]
    budget = focus_scratch_bytes((9, 11), 2)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, budget, min_free_bytes=0) as scratch:
        with prepare_focus_stack([reference, alternate], alpha, coverage, 0, scratch,
                luma=(.2126, .7152, .0722), motion_threshold=.5,
                motion_policy='review', ram_max_bytes=1024**3, tile=3) as files:
            np.testing.assert_allclose(np.load(files.rgb), reference, rtol=0, atol=1e-6)
            np.testing.assert_array_equal(np.load(files.alpha), alpha[0])
            assert files.motion_fraction == 0


@pytest.mark.parametrize('luma,policy,code', [
    ((.2126,.7152,.0722),'fallback','FOCUS_MOTION_INVALID'),
    ((1,0),'review','FOCUS_WEIGHT_INVALID'),
    ((1,-1,1),'review','FOCUS_WEIGHT_INVALID'),
    ((float('nan'),0,1),'review','FOCUS_WEIGHT_INVALID'),
    ((1,1,1),'review','FOCUS_WEIGHT_INVALID'),
    (('invalid',0,1),'review','FOCUS_WEIGHT_INVALID'),
])
def test_invalid_focus_policy_and_luma_rejected_before_scratch(luma,policy,code):
    frames,alphas,coverages=inputs()
    class NoScratch:
        def reserve(self,_size):pytest.fail('invalid focus contract allocated scratch')
    with pytest.raises(ValueError,match=code):
        with prepare_focus_stack(frames,alphas,coverages,0,NoScratch(),
                luma=luma,motion_threshold=.5,motion_policy=policy,ram_max_bytes=1024**3):
            pytest.fail('unexpected result')
