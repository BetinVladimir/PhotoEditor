import numpy as np
from photo_worker.operations.alpha.refine import build_trimap


def test_trimap_preserves_known_regions_and_expands_uncertainty():
    alpha=np.zeros((31,31),np.float32);alpha[8:23,8:23]=1;alpha[7,8:23]=.5
    trimap,boundary=build_trimap(alpha,radius=2)
    assert np.all(trimap[boundary]==.5) and trimap[15,15]==1 and trimap[0,0]==0
    assert np.count_nonzero(boundary)>15


def test_no_uncertainty_has_empty_boundary():
    alpha=np.zeros((8,8),np.float32)
    _,boundary=build_trimap(alpha)
    assert not np.any(boundary)


def test_hard_foreground_edge_still_builds_matting_band():
    alpha=np.zeros((20,20),np.float32);alpha[5:15,5:15]=1
    trimap,boundary=build_trimap(alpha,radius=2)
    assert np.any(boundary) and np.all(trimap[boundary]==.5) and trimap[10,10]==1
