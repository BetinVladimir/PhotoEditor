from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.optical.multi_prepare import prepare_planar_stack, prepare_automatic_planar_stack, multi_planar_resources
from photo_worker.runtime.scratch import ScratchSpace


@pytest.mark.parametrize('operation', ['panorama', 'focus_stack'])
def test_planar_stack_keeps_all_registrations_and_cleans_files(tmp_path, operation):
    frame = np.full((7, 9, 3), 4., np.float32); alpha = np.ones((7, 9), np.float32)
    resources = multi_planar_resources([(7, 9)]*2, (7, 9), operation, tile=3)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, resources['scratch_bytes'], min_free_bytes=0) as scratch:
        with prepare_planar_stack([frame, frame.copy()], [alpha, alpha.copy()], [np.eye(3)]*2, 0, scratch,
                operation=operation, max_pixels=63, ram_max_bytes=resources['ram_bytes'],
                motion_threshold=.5, luma=(.2126, .7152, .0722), tile=3) as prepared:
            assert prepared.shape == (7, 9) and len(prepared.registrations) == 2
            np.testing.assert_allclose(np.load(prepared.result.rgb), frame, atol=1e-6)
            paths = [prepared.result.rgb, prepared.result.alpha]+[r.rgb for r in prepared.registrations]
            assert all(p.exists() for p in paths)
        assert all(not p.exists() for p in paths)


def test_aggregate_ram_gate_precedes_any_frame_allocation():
    class NoScratch:
        budget_bytes = 2**40
        def reserve(self, _size): pytest.fail('unexpected allocation')
    frame = np.ones((7, 9, 3), np.float32); alpha = np.ones((7, 9), np.float32)
    with pytest.raises(ValueError, match='MULTI_RAM_EXCEEDED'):
        with prepare_planar_stack([frame]*2, [alpha]*2, [np.eye(3)]*2, 0, NoScratch(),
                operation='focus_stack', max_pixels=63, ram_max_bytes=1,
                motion_threshold=.5, luma=(.2126, .7152, .0722)):
            pytest.fail('unexpected result')


def test_focus_reference_cannot_be_silently_transformed():
    frame = np.ones((7, 9, 3), np.float32); alpha = np.ones((7, 9), np.float32)
    shifted = np.array([[1., 0, 1], [0, 1, 0], [0, 0, 1]])
    with pytest.raises(ValueError, match='MULTI_REFERENCE_INVALID'):
        with prepare_planar_stack([frame]*2, [alpha]*2, [shifted, np.eye(3)], 0, None,
                operation='focus_stack', max_pixels=63, ram_max_bytes=2**40,
                motion_threshold=.5, luma=(.2126, .7152, .0722)):
            pytest.fail('unexpected result')


@pytest.mark.parametrize('operation', ['panorama', 'focus_stack'])
@pytest.mark.parametrize('preparer', [prepare_planar_stack, prepare_automatic_planar_stack])
def test_planar_stack_rejects_policy_above_200mp_before_proxy_or_scratch(operation, preparer):
    frame = np.ones((7, 9, 3), np.float32); alpha = np.ones((7, 9), np.float32)
    kwargs = dict(operation=operation, max_pixels=200000001, ram_max_bytes=2**40,
                  motion_threshold=.5, luma=(.2126, .7152, .0722))
    if preparer is prepare_planar_stack:
        args = ([frame]*2, [alpha]*2, [np.eye(3)]*2, 0, None)
    else:
        args = ([frame]*2, [alpha]*2, 0, None)
        kwargs.update(registration_error_px=2., match_inlier_min=8)
    with pytest.raises(ValueError, match='MULTI_INPUT_INVALID'):
        with preparer(*args, **kwargs):
            pytest.fail('unexpected result')


@pytest.mark.parametrize('preparer', [prepare_planar_stack, prepare_automatic_planar_stack])
def test_panorama_rejects_21_frames_before_proxy_or_scratch(preparer):
    frame = np.ones((7, 9, 3), np.float32); alpha = np.ones((7, 9), np.float32)
    kwargs = dict(operation='panorama', max_pixels=200000000, ram_max_bytes=2**40,
                  motion_threshold=.5, luma=(.2126, .7152, .0722))
    if preparer is prepare_planar_stack:
        args = ([frame]*21, [alpha]*21, [np.eye(3)]*21, 0, None)
    else:
        args = ([frame]*21, [alpha]*21, 0, None)
        kwargs.update(registration_error_px=2., match_inlier_min=8)
    with pytest.raises(ValueError, match='MULTI_INPUT_INVALID'):
        with preparer(*args, **kwargs):
            pytest.fail('unexpected result')


@pytest.mark.parametrize('bad_frame,bad_alpha', [
    (np.ones((7, 9, 3), np.float64), np.ones((7, 9), np.float32)),
    (np.ones((7, 9, 3), np.float32), np.ones((7, 9), np.float64)),
    (np.ones((7, 9, 4), np.float32), np.ones((7, 9), np.float32)),
])
def test_automatic_stack_rejects_invalid_channels_or_dtype_before_proxy(bad_frame, bad_alpha):
    with pytest.raises(ValueError, match='MULTI_INPUT_INVALID'):
        with prepare_automatic_planar_stack([bad_frame]*2, [bad_alpha]*2, 0, None,
                operation='focus_stack', max_pixels=200000000, ram_max_bytes=2**40,
                registration_error_px=2., match_inlier_min=8,
                motion_threshold=.5, luma=(.2126, .7152, .0722)):
            pytest.fail('unexpected result')


@pytest.mark.parametrize('preparer', [prepare_planar_stack, prepare_automatic_planar_stack])
@pytest.mark.parametrize('operation', ['panorama', 'focus_stack'])
@pytest.mark.parametrize('threshold', [float('nan'), float('inf'), -1., True])
def test_invalid_motion_threshold_precedes_registration_and_scratch(preparer, operation, threshold):
    frame = np.ones((8, 9, 3), np.float32); alpha = np.ones((8, 9), np.float32)
    kwargs = dict(operation=operation, max_pixels=200000000, ram_max_bytes=2**40,
                  motion_threshold=threshold, luma=(.2126, .7152, .0722))
    if preparer is prepare_planar_stack:
        args = ([frame]*2, [alpha]*2, [np.eye(3)]*2, 0, None)
    else:
        args = ([frame]*2, [alpha]*2, 0, None)
        kwargs.update(registration_error_px=2., match_inlier_min=.8)
    with pytest.raises(ValueError, match='MULTI_MOTION_INVALID'):
        with preparer(*args, **kwargs):
            pytest.fail('unexpected scratch or result')


@pytest.mark.parametrize('preparer', [prepare_planar_stack, prepare_automatic_planar_stack])
def test_invalid_focus_motion_policy_precedes_registration_and_scratch(preparer):
    frame = np.ones((8, 9, 3), np.float32); alpha = np.ones((8, 9), np.float32)
    kwargs = dict(operation='focus_stack', max_pixels=200000000, ram_max_bytes=2**40,
                  motion_threshold=.5, motion_policy='ignore', luma=(.2126, .7152, .0722))
    if preparer is prepare_planar_stack:
        args = ([frame]*2, [alpha]*2, [np.eye(3)]*2, 0, None)
    else:
        args = ([frame]*2, [alpha]*2, 0, None)
        kwargs.update(registration_error_px=2., match_inlier_min=.8)
    with pytest.raises(ValueError, match='MULTI_MOTION_INVALID'):
        with preparer(*args, **kwargs):
            pytest.fail('unexpected scratch or result')
