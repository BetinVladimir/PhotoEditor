from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_layer_prepare import scratch,COLOR
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_composition import compose_render_pixels


def test_restored_pixels_do_not_keep_transient_changes_or_erase_generated_lineage(tmp_path):
    original=AdjustmentPixels(np.zeros((1,2,3),np.float32),None)
    base=AdjustmentPixels(np.ones((1,2,3),np.float32),None)
    item=SimpleNamespace(layer=SimpleNamespace(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='test.adjust'))
    masks=SimpleNamespace(layer=lambda *args:np.ones((1,2),np.float32),protection=lambda *args:None)
    @contextmanager
    def proposal(item,current):
        yield AdjustmentPixels(original.rgb.copy(),None,np.ones((1,2),np.float32))
    space=scratch(tmp_path)
    try:
        with compose_render_pixels(original,base,(item,),masks,space,proposal,color=COLOR,max_tile_bytes=4096) as result:
            assert result.changes.changed_pixels==0
            np.testing.assert_array_equal(np.load(result.changes.path),0)
            np.testing.assert_array_equal(result.pixels.generated,1)
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def test_base_style_effects_count_and_protection_checks_even_without_layers(tmp_path):
    original=AdjustmentPixels(np.zeros((1,1,3),np.float32),None)
    base=AdjustmentPixels(np.ones((1,1,3),np.float32),None)
    masks=SimpleNamespace(protection=lambda *args:None)
    space=scratch(tmp_path)
    try:
        with compose_render_pixels(original,base,(),masks,space,None,color=COLOR,max_tile_bytes=4096) as result:
            assert result.changes.changed_pixels==1 and result.changes.rgb_changed
        masks.protection=lambda *args:np.ones((1,1),np.float32)
        with pytest.raises(ValueError,match='PROTECTION_PIXEL_CHANGED'):
            with compose_render_pixels(original,base,(),masks,space,None,color=COLOR,max_tile_bytes=4096):pass
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


@pytest.mark.parametrize('failure',['shape','generated_nan','generated_range','alpha','rgb_nan','budget'])
def test_invalid_initial_pixels_reject_before_proposal_or_scratch_io(tmp_path,failure):
    original=AdjustmentPixels(np.zeros((1,2,3),np.float32),None)
    rgb=original.rgb.copy();alpha=None;generated=None;budget=4096
    if failure=='shape':rgb=np.zeros((1,1,3),np.float32)
    if failure=='generated_nan':generated=np.full((1,2),np.nan,np.float32)
    if failure=='generated_range':generated=np.full((1,2),1.01,np.float32)
    if failure=='alpha':alpha=np.zeros((1,1),np.float32)
    if failure=='rgb_nan':rgb[0,0,0]=np.nan
    if failure=='budget':budget=1
    base=AdjustmentPixels(rgb,alpha,generated)
    item=SimpleNamespace(layer=SimpleNamespace(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='test.adjust'))
    calls=[]
    @contextmanager
    def proposal(*args):
        calls.append(args)
        yield base
    space=scratch(tmp_path)
    try:
        with pytest.raises(ValueError,match='RENDER_COMPOSITION'):
            with compose_render_pixels(original,base,(item,),SimpleNamespace(),space,proposal,color=COLOR,max_tile_bytes=budget):pass
        assert calls==[] and not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()
