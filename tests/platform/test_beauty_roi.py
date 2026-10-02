import numpy as np
import pytest
from photo_worker.algorithms.beauty_roi import plan_roi
from photo_worker.algorithms.skin_mask import build_skin_mask
from photo_worker.algorithms.beauty import beauty_filter
from photo_worker.contracts.images import Canvas,Rect
from test_skin_mask import fixture
from test_beauty import params


def test_planned_roi_matches_full_frame_retouched_skin():
    face,_,_=fixture()
    # Embed the same face well inside a larger photograph.
    for p in face.points:
        p.x=(p.x*256+300)/900;p.y=(p.y*256+240)/800
    face.canvas=Canvas(width=900,height=800,orientation=1,space_id='oriented:test',source_sha256='a'*64)
    planned=plan_roi(face);r=planned.rect;sl=(slice(r.y,r.y+r.height),slice(r.x,r.x+r.width))
    rng=np.random.default_rng(10)
    rgb=rng.uniform(.2,.7,(800,900,3)).astype(np.float32)
    parsing=np.ones((800,900),np.float32)
    full_mask=build_skin_mask(face,Rect(x=0,y=0,width=900,height=800),parsing)
    local_mask=build_skin_mask(face,r,parsing[sl])
    assert np.array_equal(local_mask.skin,full_mask.skin[sl])
    full=beauty_filter(rgb,full_mask.skin,full_mask.protected,planned.face_width,params())
    local=beauty_filter(rgb[sl],local_mask.skin,local_mask.protected,planned.face_width,params())
    assert np.max(np.abs(full.rgb[sl]-local.rgb))<2e-6
    assert r.width*r.height<900*800


def test_real_frame_edges_and_budget_are_explicit():
    face,_,_=fixture();plan=plan_roi(face)
    r=plan.rect
    assert r.x>=0 and r.y>=0 and r.x+r.width<=256 and r.y+r.height<=256
    with pytest.raises(ValueError,match='BUDGET'):plan_roi(face,max_work_pixels=100)
    for p in face.points:p.y=.5
    with pytest.raises(ValueError,match='DEGENERATE'):plan_roi(face)
