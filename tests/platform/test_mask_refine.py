import numpy as np
import pytest
from photo_worker.algorithms.mask_refine import refine_tiles
from photo_worker.algorithms.tiles import guided_filter
from photo_worker.algorithms.color import LUMA
from photo_worker.contracts.images import Rect


def collect(mask,guide,**kwargs):
    output=np.empty_like(mask)
    for part in refine_tiles(mask,guide,**kwargs):
        h,w=part.values.shape;output[part.y:part.y+h,part.x:part.x+w]=part.values
        assert not part.values.flags.writeable
    return output


def test_full_reference_and_tile_seams():
    rng=np.random.default_rng(14)
    guide=rng.random((311,607,3),dtype=np.float32);mask=rng.random((311,607),dtype=np.float32)
    luma=np.sum(guide*LUMA.astype(np.float32),axis=-1,dtype=np.float32)
    expected=np.clip(guided_filter(luma,mask,8,1e-4),0,1)
    for tile in (256,512,1024):
        result=collect(mask,guide,radius=8,tile_size=tile)
        assert np.max(np.abs(result-expected))<1e-6


def test_guide_edge_and_valid_region():
    guide=np.zeros((64,64,3),np.float32);guide[:,32:]=1
    mask=np.zeros((64,64),np.float32);mask[:,32:]=1
    output=collect(mask,guide,radius=8)
    assert np.max(np.abs(output-mask))<.01
    region=Rect(x=10,y=10,width=40,height=40)
    output=collect(mask,guide,radius=8,valid_region=region)
    assert not np.any(output[:10]) and not np.any(output[:,50:])


def test_strided_invalid_budget_and_cancel():
    guide=np.ones((30,40,3),np.float32)[:,::2];mask=np.ones((30,40),np.float32)[:,::2]
    assert np.array_equal(collect(mask,guide),mask)
    with pytest.raises(ValueError,match='BUDGET'):collect(mask,guide,max_work_pixels=1)
    bad=mask.copy();bad[1,1]=np.nan
    with pytest.raises(ValueError):collect(bad,guide)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):next(refine_tiles(mask,guide,check_active=cancel))
