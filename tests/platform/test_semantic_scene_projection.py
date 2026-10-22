"""Authored conservative full-source projection scenarios; not executed."""
import numpy as np
import pytest
from photo_worker.algorithms.semantic_scene_projection import project_scene_source,require_full_depth_plane


def source():
    shape=(512,768)
    return [np.full((*shape,3),.4,np.float32),np.ones(shape,np.uint16),np.full(shape,.5,np.float32),np.zeros(shape,np.float32),np.zeros(shape,np.float32)]


def test_single_positive_and_unsafe_source_pixels_survive_downsampling():
    values=source();values[3][257,257]=np.nextafter(np.float32(0),np.float32(1));values[4][258,258]=.001;values[1][259,259]=12
    projected=project_scene_source(*values,32,frozenset({1}))
    assert np.count_nonzero(projected[3])==1 and np.count_nonzero(projected[4])==1
    assert np.count_nonzero(projected[1]==0)==1
    assert projected[5][0,0]==32/768 and projected[5][1,1]==32/512


def test_full_depth_residual_does_not_lose_subproxy_bump():
    depth=np.full((512,768),.5,np.float32);depth[257,257]=.8
    # Average bump would be <.01 in a32² proxy. Fullresolution gate still refuses.
    with pytest.raises(ValueError,match='NONPLANAR'):
        require_full_depth_plane(depth,np.ones((32,32),bool),32,.01)


def test_full_depth_plane_positive_and_tile_cancellation():
    y,x=np.mgrid[:512,:768];depth=(.2+.1*x/767+.1*y/511).astype(np.float32)
    require_full_depth_plane(depth,np.ones((32,32),bool),32,.0001)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==4:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        project_scene_source(*source(),32,frozenset({1}),check_active=active)
