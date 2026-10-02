import numpy as np
import pytest
from test_layer_prepare import scratch
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_changes import prepare_render_changes


def test_final_change_map_detects_bits_and_alpha_without_transient_union(tmp_path):
    space=scratch(tmp_path);rgb=np.zeros((1,3,3),np.float32);final=rgb.copy();final[0,0,0]=-0.
    alpha=np.ones((1,3),np.float32);alpha[0,1]=.5
    try:
        with prepare_render_changes(AdjustmentPixels(rgb,None),AdjustmentPixels(final,alpha),space,max_tile_bytes=4096) as evidence:
            np.testing.assert_array_equal(np.load(evidence.path),[[1,1,0]])
            assert evidence.changed_pixels==2 and evidence.rgb_changed and evidence.alpha_changed
        with prepare_render_changes(AdjustmentPixels(rgb,None),AdjustmentPixels(rgb.copy(),np.ones((1,3),np.float32)),space,max_tile_bytes=4096) as evidence:
            assert evidence.changed_pixels==0 and not evidence.alpha_changed
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def test_any_positive_protection_rejects_changed_final_pixel_and_cleans_up(tmp_path):
    space=scratch(tmp_path);rgb=np.zeros((1,1,3),np.float32)
    try:
        with pytest.raises(ValueError,match='PROTECTION_PIXEL_CHANGED'):
            with prepare_render_changes(AdjustmentPixels(rgb,None),AdjustmentPixels(rgb+1,None),space,max_tile_bytes=4096,read_protected=lambda *args:np.full((1,1),1e-9,np.float32)):pass
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def test_nonfinite_output_and_insufficient_budget_fail_without_leaked_files(tmp_path):
    space=scratch(tmp_path);rgb=np.zeros((1,1,3),np.float32)
    try:
        for after,budget in [(rgb+np.nan,4096),(rgb,1)]:
            with pytest.raises(ValueError):
                with prepare_render_changes(AdjustmentPixels(rgb,None),AdjustmentPixels(after,None),space,max_tile_bytes=budget):pass
            assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def test_invalid_rgb_and_consumer_failure_leave_no_scratch_files(tmp_path):
    space=scratch(tmp_path);rgb=np.zeros((1,1,3),np.float32)
    try:
        with pytest.raises(ValueError,match='RENDER_CHANGE_INPUT_INVALID'):
            with prepare_render_changes(AdjustmentPixels(None,None),AdjustmentPixels(rgb,None),space,max_tile_bytes=4096):pass
        with pytest.raises(RuntimeError,match='consumer'):
            with prepare_render_changes(AdjustmentPixels(rgb,None),AdjustmentPixels(rgb,None),space,max_tile_bytes=4096):
                raise RuntimeError('consumer')
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def test_cancellation_after_yield_releases_mask(tmp_path):
    space=scratch(tmp_path);rgb=np.zeros((1,1,3),np.float32);cancelled=False
    def check():
        if cancelled:raise RuntimeError('cancelled')
    try:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_render_changes(AdjustmentPixels(rgb,None),AdjustmentPixels(rgb,None),space,max_tile_bytes=4096,check_active=check):
                cancelled=True
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()
