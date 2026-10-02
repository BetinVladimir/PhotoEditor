import numpy as np
import pytest
from uuid import UUID
from photo_worker.operations.optical.contracts import LensProfile
from photo_worker.algorithms.lens import lens_map_tiles,match_lens_metadata,lens_points,lens_forward_point,validate_lens_geometry

def profile(**changes):
    return LensProfile(schema_version=1,camera_model='Camera',lens_model='Lens',
        focal_mm=50,aperture=2,cx=.5,cy=.5,k=changes.get('k',[0,0,0]),p=[0,0],
        vignette=[0,0,0],ca_red_scale=1,ca_blue_scale=1,
        qualification={'artifact_id':UUID(int=1),'sha256':'a'*64})

def test_identity_centers_and_tiled_map_agree():
    p=profile();full=next(lens_map_tiles(p,(2,4),1,tile=4))
    x,y=np.meshgrid(np.arange(4)+.5,np.arange(2)+.5)
    np.testing.assert_array_equal(full.green,np.stack((x,y),axis=-1))
    np.testing.assert_array_equal(full.gain,np.ones((2,4)))
    p=profile(k=[.2,0,0]);whole=next(lens_map_tiles(p,(2,4),1,tile=4));assembled=np.zeros_like(whole.green)
    for block in lens_map_tiles(p,(2,4),1,tile=1):assembled[block.y:block.y+1,block.x:block.x+1]=block.green
    np.testing.assert_array_equal(whole.green,assembled)
    bypass=next(lens_map_tiles(p,(2,4),0,tile=4));np.testing.assert_array_equal(bypass.green,full.green)

def test_metadata_is_exact_and_never_interpolated():
    p=profile();match_lens_metadata(p,'Camera','Lens',50,2)
    for camera,lens,focal,aperture in [('Other','Lens',50,2),('Camera','Lens',51,2),('Camera','Lens',50,2.1)]:
        with pytest.raises(ValueError,match='LENS_PROFILE_MISMATCH'):match_lens_metadata(p,camera,lens,focal,aperture)


def test_forward_point_round_trip_and_fold_refusal():
    p=profile(k=[.2,0,0]);point=(2.0,1.0)
    source=tuple(lens_points(p,(4,6),1,np.array([point]))[0])
    recovered=lens_forward_point(p,(4,6),1,source)
    np.testing.assert_allclose(recovered,point,atol=1e-4,rtol=0)
    assert lens_forward_point(p,(4,6),1,(-1,2)) is None
    folded=profile(k=[-2,0,0])
    with pytest.raises(ValueError,match='LENS_NON_INVERTIBLE'):
        validate_lens_geometry(folded,(4,4),1)
    with pytest.raises(ValueError,match='LENS_NON_INVERTIBLE'):
        next(lens_map_tiles(folded,(4,4),1))
    assert lens_forward_point(folded,(4,4),1,(2,2)) is None
    validate_lens_geometry(folded,(4,4),0)


def test_linear_hdr_alpha_and_exact_bypass():
    from photo_worker.algorithms.lens import resample_lens_tiles
    rgb=np.array([[[4,-2,3],[99,77,55]]],dtype=np.float32)
    alpha=np.array([[1,0]],dtype=np.float32)
    original=rgb.copy()
    result=next(resample_lens_tiles(profile(),rgb,alpha,1))
    np.testing.assert_array_equal(result.rgb[0,0],rgb[0,0])
    np.testing.assert_array_equal(result.rgb[0,1],0)
    assert not result.uncovered
    bypass=next(resample_lens_tiles(profile(k=[-2,0,0]),rgb,alpha,0))
    np.testing.assert_array_equal(bypass.rgb,rgb)
    np.testing.assert_array_equal(bypass.alpha,alpha)
    np.testing.assert_array_equal(rgb,original)


def test_premultiplied_sampling_ignores_hidden_color():
    from photo_worker.algorithms.lens import _sample_channel
    rgb=np.array([[[2,0,0],[100,0,0]]],dtype=np.float32)
    alpha=np.array([[1,0]],dtype=np.float32)
    color,opacity,covered=_sample_channel(rgb,alpha,np.array([[[1.,.5]]],dtype=np.float32),0)
    np.testing.assert_array_equal(color,[[2]])
    np.testing.assert_array_equal(opacity,[[.5]])
    assert covered.all()


def test_resampling_tile_invariance_and_uncovered_canvas():
    from photo_worker.algorithms.lens import resample_lens_tiles
    rgb=np.arange(48,dtype=np.float32).reshape(4,4,3)/10
    alpha=np.ones((4,4),dtype=np.float32)
    p=profile(k=[.2,0,0])
    whole=next(resample_lens_tiles(p,rgb,alpha,1,tile=4))
    assembled=np.empty_like(rgb);opacity=np.empty_like(alpha)
    for block in resample_lens_tiles(p,rgb,alpha,1,tile=1):
        assembled[block.y:block.y+1,block.x:block.x+1]=block.rgb
        opacity[block.y:block.y+1,block.x:block.x+1]=block.alpha
    np.testing.assert_array_equal(assembled,whole.rgb)
    np.testing.assert_array_equal(opacity,whole.alpha)
    assert whole.uncovered
    np.testing.assert_array_equal(whole.rgb[whole.alpha==0],0)
    with pytest.raises(ValueError,match='LENS_IMAGE_INVALID'):
        next(resample_lens_tiles(p,rgb,alpha.astype(np.float64),1))


def test_ca_coverage_is_conservative_and_vignette_does_not_clip_hdr():
    from photo_worker.algorithms.lens import resample_lens_tiles
    rgb=np.full((3,3,3),2,dtype=np.float32);alpha=np.ones((3,3),dtype=np.float32)
    p=profile().model_copy(update={'ca_red_scale':1.01,'vignette':[-1,0,0]})
    block=next(resample_lens_tiles(p,rgb,alpha,1))
    assert block.uncovered
    assert block.alpha[0,0]==0
    np.testing.assert_array_equal(block.rgb[0,0],0)
    np.testing.assert_array_equal(block.rgb[1,1],[2,2,2])
    p=p.model_copy(update={'ca_red_scale':1.0})
    block=next(resample_lens_tiles(p,rgb,alpha,1))
    assert block.rgb[0,0,0]>2
    assert not block.uncovered


def test_mask_follows_green_centers_and_ignores_ca_gain():
    from photo_worker.algorithms.lens import resample_lens_mask_tiles
    mask=np.arange(12,dtype=np.float32).reshape(3,4)/12
    p=profile().model_copy(update={'ca_red_scale':1.01,'vignette':[-1,0,0]})
    identity=next(resample_lens_mask_tiles(p,mask,1))
    np.testing.assert_array_equal(identity.values,mask)
    p=p.model_copy(update={'k':[.2,0,0]})
    whole=next(resample_lens_mask_tiles(p,mask,1,tile=4))
    assembled=np.empty_like(mask)
    for block in resample_lens_mask_tiles(p,mask,1,tile=1):
        assembled[block.y:block.y+1,block.x:block.x+1]=block.values
    np.testing.assert_array_equal(assembled,whole.values)
    assert whole.values[0,0]==0
    assert ((whole.values>=0)&(whole.values<=1)).all()
    invalid=mask.copy();invalid[0,0]=np.nan
    with pytest.raises(ValueError,match='LENS_MASK_INVALID'):
        next(resample_lens_mask_tiles(p,invalid,1))
