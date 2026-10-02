from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas,Rect,MatrixTransform,ResizeTransform,CropTransform,DenseWarpTransform
from photo_worker.algorithms.mask_transform import transform_tiles


def canvas(w,h,name='source'):return Canvas(width=w,height=h,orientation=1,space_id=name,source_sha256='a'*64)
def common(w=601,h=311,tw=None,th=None):
    tw=tw or w;th=th or h
    return dict(from_canvas=canvas(w,h),to_canvas=canvas(tw,th,'target'),invertibility='invertible',valid_region=Rect(x=0,y=0,width=tw,height=th))
def collect(source,step,tile=256,**kwargs):
    out=np.empty((step.to_canvas.height,step.to_canvas.width),source.dtype)
    for t in transform_tiles(source,step,tile_size=tile,**kwargs):
        h,w=t.values.shape;out[t.y:t.y+h,t.x:t.x+w]=t.values
        assert not t.values.flags.writeable
    return out


def test_identity_crop_and_resize_edges():
    a=np.arange(20,dtype=np.float32).reshape(4,5)/20
    step=MatrixTransform(kind='matrix',forward_matrix=[1.,0.,0.,0.,1.,0.,0.,0.,1.],**common(5,4))
    assert np.array_equal(collect(a,step),a)
    crop=CropTransform(kind='crop',crop_rect=Rect(x=1,y=1,width=3,height=2),**common(5,4,3,2))
    assert np.array_equal(collect(a,crop),a[1:3,1:4])
    resize=ResizeTransform(kind='resize',pixel_centers='half_pixel',**common(5,4,10,8))
    out=collect(a,resize)
    assert out[0,0]==a[0,0] and out[-1,-1]==a[-1,-1]


def test_fractional_shift_exact_and_tile_invariance():
    source=np.random.default_rng(2).random((311,601),dtype=np.float32)
    step=MatrixTransform(kind='matrix',forward_matrix=[1.,0.,.5,0.,1.,0.,0.,0.,1.],**common())
    result=collect(source,step)
    assert np.array_equal(result[:,1:],((source[:,:-1].astype(np.float64)+source[:,1:])/2).astype(np.float32))
    assert np.array_equal(result,collect(source,step,512))
    assert np.array_equal(result,collect(source,step,1024))


def test_dense_field_labels_and_invalid_region():
    source=np.arange(20,dtype=np.uint16).reshape(4,5)
    field=np.stack(np.meshgrid(np.arange(5),np.arange(4)),axis=-1).astype(np.float32)
    args=common(5,4);args['valid_region']=Rect(x=1,y=1,width=3,height=2)
    step=DenseWarpTransform(kind='dense_warp',inverse_field=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),**args)
    out=collect(source,step,labels=True,inverse_field=field)
    expected=np.zeros_like(source);expected[1:3,1:4]=source[1:3,1:4]
    assert np.array_equal(out,expected)
    field[0,0,0]=np.nan
    with pytest.raises(ValueError,match='Non-finite'):collect(source,step,labels=True,inverse_field=field)


def test_bad_probability_and_cancellation():
    step=ResizeTransform(kind='resize',pixel_centers='half_pixel',**common(2,2))
    source=np.ones((2,2),np.float32);source[0,0]=np.nan
    with pytest.raises(ValueError):collect(source,step)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):next(transform_tiles(source,step,check_active=cancel))


def test_dense_half_pixel_marker_preserves_legacy_hash_and_identity():
    from photo_worker.contracts.images import TransformChain
    from photo_worker.contracts.base import canonical_hash
    source=np.arange(20,dtype=np.float32).reshape(4,5)/20
    field=np.stack(np.meshgrid(np.arange(5),np.arange(4)),axis=-1).astype(np.float32)
    legacy=DenseWarpTransform(kind='dense_warp',inverse_field=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),**common(5,4))
    raw=legacy.model_dump(mode='json')
    assert 'pixel_centers' not in raw
    chain=TransformChain.create([legacy])
    assert chain.hash==canonical_hash({'steps':[raw]})
    assert TransformChain.model_validate_json(chain.model_dump_json()).hash==chain.hash
    assert np.array_equal(collect(source,legacy,inverse_field=field),source)
    centered=DenseWarpTransform(kind='dense_warp',inverse_field=legacy.inverse_field,pixel_centers='half_pixel',**common(5,4))
    assert np.array_equal(collect(source,centered,inverse_field=field+.5),source)
    assert TransformChain.create([centered]).hash!=chain.hash
    with pytest.raises(ValueError):DenseWarpTransform.model_validate({**raw,'pixel_centers':None})


def test_lens_dense_consumer_matches_green_sampler():
    from photo_worker.operations.optical.contracts import LensProfile
    from photo_worker.algorithms.lens import lens_map_tiles,resample_lens_mask_tiles
    source=np.arange(20,dtype=np.float32).reshape(4,5)/20
    profile=LensProfile(schema_version=1,camera_model='Camera',lens_model='Lens',
        focal_mm=50,aperture=2,cx=.5,cy=.5,k=[.2,0,0],p=[0,0],
        vignette=[0,0,0],ca_red_scale=1,ca_blue_scale=1,
        qualification=ArtifactRef(artifact_id=uuid4(),sha256='c'*64))
    field=next(lens_map_tiles(profile,(4,5),1)).green
    step=DenseWarpTransform(kind='dense_warp',inverse_field=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),
        pixel_centers='half_pixel',**common(5,4))
    expected=next(resample_lens_mask_tiles(profile,source,1)).values
    np.testing.assert_allclose(collect(source,step,inverse_field=field),expected,rtol=0,atol=1e-7)
