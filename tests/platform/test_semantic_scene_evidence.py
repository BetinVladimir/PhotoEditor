"""Authored native classical scene checks; no qualification asserted."""
import numpy as np
import pytest
from photo_worker.algorithms.semantic_scene import ScenePolicy,verify_planar_scene_proxies


def scene():
    rng=np.random.default_rng(2026)
    image=rng.integers(0,256,(64,64),dtype=np.uint8)
    labels=np.ones((64,64),dtype=np.uint8)
    y,x=np.mgrid[:64,:64]
    depth=(.2+x*.002+y*.001).astype(np.float32)
    mask=np.zeros((64,64),dtype=np.float32);mask[16:48,16:48]=1
    zero=np.zeros_like(mask)
    return [image,image.copy(),labels,labels.copy(),depth,depth.copy(),mask,zero,zero.copy(),np.eye(3)]


def policy():return ScenePolicy(frozenset({1}),32,.2,.2,.001)


def test_native_identity_textured_background_with_planar_depth():
    # Real cv2 Farneback execution on deterministic texture, not a fake renderer.
    verify_planar_scene_proxies(*scene(),policy=policy())


def test_target_planarity_domain_is_actual_warp_and_flow_footprint():
    values=scene();values[5][0,0]=.9;values[3][0,0]=12
    domain=verify_planar_scene_proxies(*values,policy=policy())
    assert domain.dtype==np.bool_ and domain.shape==(64,64)
    assert domain[20,20] and not domain[0,0]
    from photo_worker.algorithms.semantic_scene_projection import require_full_depth_plane
    # Every original pixel in each touched proxy bin is checked. A depth
    # discontinuity outside that actual footprint is unrelated foreground.
    y,x=np.mgrid[:256,:256];depth=(.2+x*.0005+y*.00025).astype(np.float32)
    depth[0,0]=.9
    require_full_depth_plane(depth,domain,64,.001)
    depth[80,80]=.9
    with pytest.raises(ValueError,match='NONPLANAR'):
        require_full_depth_plane(depth,domain,64,.001)


@pytest.mark.parametrize('index,change,reason',[
    (2,lambda a:a.__setitem__((20,20),12),'FOREGROUND'),
    (3,lambda a:a.__setitem__((20,20),12),'FOREGROUND'),
    (7,lambda a:a.__setitem__((20,20),np.nextafter(np.float32(0),np.float32(1))),'PROTECTED'),
    (8,lambda a:a.__setitem__((20,20),.001),'PROTECTED'),
    (4,lambda a:a.__setitem__((20,20),.9),'NONPLANAR'),
    (5,lambda a:a.__setitem__((20,20),.9),'NONPLANAR'),
])
def test_physical_unsafe_evidence_refuses(index,change,reason):
    values=scene();change(values[index])
    with pytest.raises(ValueError,match=reason):verify_planar_scene_proxies(*values,policy=policy())


def test_target_movement_inconsistent_with_identity_homography_refuses():
    values=scene();values[1]=np.roll(values[0],5,axis=1)
    with pytest.raises(ValueError,match='PARALLAX_OR_OCCLUSION'):
        verify_planar_scene_proxies(*values,policy=policy())


def test_cancellation_during_scene_work():
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==8:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        verify_planar_scene_proxies(*scene(),policy=policy(),check_active=active)


def test_missing_background_policy_has_no_permissive_default():
    with pytest.raises(ValueError,match='POLICY_INVALID'):ScenePolicy(frozenset(),32,.2,.2,.001)


@pytest.mark.parametrize('protected',[True,False])
def test_actual_homography_footprint_is_checked_even_within_flow_tolerance(protected):
    values=scene()
    # q is a tolerated fractional step across the left support edge;
    # identity flow need not sample that neighboring pixel.
    values[6][:]=0;values[6][16:48,16:48]=1
    values[9][0,2]=-.1
    if protected:values[8][20,15]=.01
    else:values[3][20,15]=12
    with pytest.raises(ValueError,match='PROTECTED' if protected else 'FOREGROUND'):
        verify_planar_scene_proxies(*values,policy=policy())


def test_actual_scene_parse_uint16_labels_are_supported_without_narrowing():
    values=scene();values[2]=values[2].astype('<u2');values[3]=values[3].astype('<u2')
    verify_planar_scene_proxies(*values,policy=policy())
    values[2][20,20]=257
    with pytest.raises(ValueError,match='LABELS_INVALID'):
        verify_planar_scene_proxies(*values,policy=policy())
