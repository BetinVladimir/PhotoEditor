from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import Canvas,TransformChain,Face68,Landmark,Rect
from photo_worker.algorithms.skin_mask import build_skin_mask


def fixture():
    points=[]
    for theta in np.linspace(np.pi,0,17):points.append((128+90*np.cos(theta),105+105*np.sin(theta)))
    points.extend([(70,85),(80,80),(90,78),(100,80),(110,85),(146,85),(156,80),(166,78),(176,80),(186,85)])
    points.extend([(128,95),(128,110),(128,125),(128,140),(110,145),(119,150),(128,152),(137,150),(146,145)])
    for cx in (90,166):
        points.extend([(cx+18*np.cos(t),104+9*np.sin(t)) for t in np.linspace(0,2*np.pi,6,endpoint=False)])
    points.extend([(128+32*np.cos(t),179+12*np.sin(t)) for t in np.linspace(0,2*np.pi,20,endpoint=False)])
    face=Face68(object_id=uuid4(),points=[Landmark(x=(x+.5)/256,y=(y+.5)/256,confidence=1.) for x,y in points],
                canvas=Canvas(width=256,height=256,orientation=1,space_id='oriented:test',source_sha256='a'*64),transform_chain=TransformChain.create([]))
    return face,Rect(x=0,y=0,width=256,height=256),np.ones((256,256),np.float32)


def test_skin_excludes_features_hair_and_permanent_marks():
    face,roi,parsing=fixture();parsing[:70]=0
    permanent=np.zeros_like(parsing);permanent[140:155,75:90]=1
    result=build_skin_mask(face,roi,parsing,permanent=permanent)
    assert result.reason is None and np.any(result.skin>.5)
    for x,y in [(90,104),(166,104),(128,135),(128,179),(90,82)]:assert result.skin[y,x]==0 and result.protected[y,x]==1
    assert not np.any(result.skin[:70]) and not np.any(result.skin[140:155,75:90])
    assert not result.skin.flags.writeable and not result.protected.flags.writeable


def test_explicit_manual_mode_and_confidence_gate():
    face,roi,mask=fixture()
    with pytest.raises(ValueError,match='parsing'):build_skin_mask(face,roi,None)
    with pytest.raises(ValueError,match='explicit'):build_skin_mask(face,roi,None,manual_mode=True)
    face.points[0].confidence=.4
    result=build_skin_mask(face,roi,mask)
    assert result.reason=='low_confidence' and not np.any(result.skin)
    result=build_skin_mask(face,roi,None,manual=mask,manual_mode=True)
    assert result.reason is None and result.skin[104,90]==0


def test_roi_budget_invalid_input_and_cancel():
    face,roi,mask=fixture()
    with pytest.raises(ValueError):build_skin_mask(face,roi,mask,max_work_pixels=1)
    mask[0,0]=np.nan
    with pytest.raises(ValueError):build_skin_mask(face,roi,mask)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):build_skin_mask(face,roi,None,check_active=cancel)


def test_soft_permanent_protection_is_not_squared():
    from photo_worker.algorithms.masks import compose
    face,roi,parsing=fixture()
    baseline=build_skin_mask(face,roi,parsing)
    partial=np.full_like(parsing,.5)
    protected=build_skin_mask(face,roi,parsing,permanent=partial)
    assert np.array_equal(protected.skin,baseline.skin)
    assert np.allclose(compose(protected.skin,protected=protected.protected),baseline.skin*.5)


def test_degenerate_landmarks_skip_before_large_morphology():
    face,roi,mask=fixture()
    for p in face.points:p.y=.5
    result=build_skin_mask(face,roi,mask)
    assert result.reason=='degenerate_landmarks' and not np.any(result.skin)
