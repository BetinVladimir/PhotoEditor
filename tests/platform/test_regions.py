import numpy as np
import pytest
from photo_worker.algorithms.regions import apply_regions
from photo_worker.algorithms.color import to_linear, LUMA, ColorDiagnostics
from photo_worker.contracts.style import RegionAdjustment

def rule(name='a', **changes):
    return RegionAdjustment(region_id=name,selector='manual',ev=changes.get('ev',0.),
        saturation=changes.get('saturation',1.),hue_deg=changes.get('hue_deg',0.),
        strength=changes.get('strength',1.),priority=changes.get('priority',0))

def test_ev_priority_overlap_and_permutation():
    rgb=np.full((3,5,3),.3,np.float32);mask=np.ones((3,5),np.float32)
    regions=[(rule('a',ev=1.),mask),(rule('b',ev=-1.),mask)]
    assert np.array_equal(apply_regions(rgb,regions).rgb,rgb)
    regions=[(rule('a',ev=1.,priority=1),mask*.5),(rule('b',ev=-1.),mask)]
    assert np.array_equal(apply_regions(rgb,regions).rgb,rgb)
    regions[0]=(rule('a',ev=1.,priority=1),mask)
    output=apply_regions(rgb,regions).rgb
    assert np.allclose(to_linear(output),to_linear(rgb)*2,atol=1e-7)
    assert np.array_equal(output,apply_regions(rgb,list(reversed(regions))).rgb)

def test_hue_preserves_linear_luminance_and_zero_support_exact():
    rgb=np.broadcast_to(np.array([.5,.4,.3],np.float32),(3,5,3)).copy()
    mask=np.ones((3,5),np.float32);mask[:,0]=0
    output=apply_regions(rgb,[(rule(hue_deg=15.),mask)]).rgb
    assert not np.array_equal(output[:,1:],rgb[:,1:])
    assert np.allclose(to_linear(output)@LUMA,to_linear(rgb)@LUMA,atol=2e-8)
    assert np.array_equal(output[:,0],rgb[:,0])

def test_tile_equivalence_missing_and_clipping():
    rgb=np.random.default_rng(4).random((19,37,3),dtype=np.float32)
    mask=np.random.default_rng(5).random((19,37),dtype=np.float32)
    r=rule(ev=1.,hue_deg=-15.,saturation=1.3)
    diagnostic=ColorDiagnostics();full=apply_regions(rgb,[(r,mask),(rule('missing'),None)],diagnostic)
    result=np.empty_like(rgb)
    for y in range(0,19,7):
        for x in range(0,37,7):
            result[y:y+7,x:x+7]=apply_regions(rgb[y:y+7,x:x+7],[(r,mask[y:y+7,x:x+7])]).rgb
    assert np.array_equal(result,full.rgb)
    assert full.skipped==('missing',) and diagnostic.clipping['regions']['above_one']>0
    assert np.min(result)>=0 and np.max(result)<=1

def test_invalid_mask_duplicate_and_cancellation():
    rgb=np.zeros((2,3,3),np.float32)
    for mask in [np.ones((2,2),np.float32),np.full((2,3),np.nan),np.full((2,3),1.1)]:
        with pytest.raises(ValueError):apply_regions(rgb,[(rule(),mask)])
    with pytest.raises(ValueError):apply_regions(rgb,[(rule(),None),(rule(),None)])
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):apply_regions(rgb,[],check_active=cancel)
