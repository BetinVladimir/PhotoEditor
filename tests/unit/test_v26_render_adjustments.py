from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_layer_prepare import scratch,COLOR
from photo_worker.v2.render_adjustments import compose_render_adjustments,AdjustmentPixels


def fixture():
    source=np.array([[[-0.,2.,-1.],[1.,1.,1.]]],np.float32)
    layers=tuple(SimpleNamespace(layer=SimpleNamespace(id=uuid4(),enabled=True,opacity=.5,blend='normal',operation='test.adjust')) for _ in range(2))
    masks=SimpleNamespace(layer=lambda id,x,y,w,h:np.ones((h,w),np.float32),protection=lambda x,y,w,h:np.array([[.001,0]],np.float32)[y:y+h,x:x+w])
    return source,layers,masks


def test_ordered_adjustments_protect_exact_bits_and_release_intermediate_files(tmp_path):
    source,layers,masks=fixture();original=source.copy();space=scratch(tmp_path);seen=[]
    @contextmanager
    def proposal(item,current):
        assert not current.rgb.flags.writeable
        seen.append(current.rgb.copy())
        yield AdjustmentPixels(np.full_like(current.rgb,3),None)
    try:
        with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256) as result:
            np.testing.assert_array_equal(result.rgb[:,0].view(np.uint32),original[:,0].view(np.uint32))
            np.testing.assert_allclose(result.rgb[:,1],2.5)
            np.testing.assert_allclose(seen[1][:,1],2)
            assert not result.rgb.flags.writeable
        np.testing.assert_array_equal(source.view(np.uint32),original.view(np.uint32))
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()


def test_proposal_failure_closes_current_outputs_and_geometry_preflight_skips_io(tmp_path):
    source,layers,masks=fixture();space=scratch(tmp_path);calls=[]
    @contextmanager
    def proposal(item,current):
        calls.append(item)
        if len(calls)==2:raise RuntimeError('operator failed')
        yield AdjustmentPixels(np.full_like(current.rgb,3),None)
    try:
        with pytest.raises(RuntimeError):
            with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256):pass
        assert not list(tmp_path.rglob('*.npy'))
        layers[0].layer.operation='optical.lens';calls.clear()
        with pytest.raises(ValueError,match='GEOMETRY_STAGE'):
            with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096):pass
        assert calls==[]
    finally:space.cleanup()


def test_generated_dependency_survives_identical_pixels_and_full_replacement_removes_it(tmp_path):
    source,layers,masks=fixture();space=scratch(tmp_path)
    masks.protection=lambda x,y,w,h:None
    layers[0].layer.opacity=1;layers[1].layer.opacity=1
    count=0
    @contextmanager
    def proposal(item,current):
        nonlocal count
        count+=1
        generated=np.ones(current.rgb.shape[:2],np.float32) if count==1 else None
        if count==2:np.testing.assert_array_equal(current.generated,1)
        yield AdjustmentPixels(current.rgb.copy(),None,generated)
    try:
        with compose_render_adjustments(source,None,layers,masks,space,proposal,color=COLOR,max_tile_bytes=4096,tile_size=256) as result:
            np.testing.assert_array_equal(result.rgb.view(np.uint32),source.view(np.uint32))
            np.testing.assert_array_equal(result.generated,0)
            assert not result.generated.flags.writeable
        assert not list(tmp_path.rglob('*.npy'))
    finally:space.cleanup()
