import numpy as np
import pytest
from photo_worker.algorithms.generated_support import generated_support_tiles
from photo_worker.contracts.images import Canvas, Rect, CropTransform, DenseWarpTransform, ResizeTransform
from photo_worker.contracts.base import ArtifactRef
from uuid import uuid4


def canvas(width,height,name):
    return Canvas(width=width,height=height,orientation=1,space_id=name,source_sha256='a'*64)


def assemble(source,step,operation,field=None):
    target=np.zeros((step.to_canvas.height,step.to_canvas.width),np.float32)
    for x,y,part in generated_support_tiles(source,step,operation=operation,inverse_field=field):
        target[y:y+part.shape[0],x:x+part.shape[1]]=part
    return target


def test_crop_preserves_binary_support_without_guided_spread():
    source=np.zeros((4,4),np.float32);source[1,1]=1
    step=CropTransform(kind='crop',from_canvas=canvas(4,4,'before'),to_canvas=canvas(2,2,'after'),
        invertibility='partial',valid_region=Rect(x=0,y=0,width=2,height=2),crop_rect=Rect(x=1,y=1,width=2,height=2))
    result=assemble(source,step,'geometry.crop')
    assert np.array_equal(result,np.array([[1,0],[0,0]],np.float32))


def test_body_tracks_every_nonzero_bilinear_contributor_not_nearest():
    source=np.zeros((2,2),np.float32);source[0,1]=1
    shape=canvas(2,2,'before');target=canvas(2,2,'after')
    step=DenseWarpTransform(kind='dense_warp',from_canvas=shape,to_canvas=target,
        invertibility='invertible',valid_region=Rect(x=0,y=0,width=2,height=2),
        inverse_field=ArtifactRef(artifact_id=uuid4(),sha256='b'*64))
    field=np.zeros((2,2,2),np.float32);field[:]=[.1,.1]
    assert np.all(assemble(source,step,'geometry.body',field)==1)
    field[:]=[0,0]
    assert np.all(assemble(source,step,'geometry.body',field)==0)


def test_neural_resize_conservatively_retains_nonlocal_ai_influence():
    source=np.zeros((2,2),np.float32);source[0,0]=1
    step=ResizeTransform(kind='resize',from_canvas=canvas(2,2,'before'),to_canvas=canvas(4,4,'after'),
        invertibility='partial',valid_region=Rect(x=0,y=0,width=4,height=4),pixel_centers='half_pixel')
    assert np.all(assemble(source,step,'image.upscale')==1)
    assert np.all(assemble(np.zeros_like(source),step,'image.upscale')==0)


def test_unqualified_geometry_or_fuzzy_mask_is_refused():
    step=CropTransform(kind='crop',from_canvas=canvas(2,2,'before'),to_canvas=canvas(1,1,'after'),
        invertibility='partial',valid_region=Rect(x=0,y=0,width=1,height=1),crop_rect=Rect(x=0,y=0,width=1,height=1))
    with pytest.raises(ValueError):assemble(np.full((2,2),.5,np.float32),step,'geometry.crop')
    with pytest.raises(ValueError):assemble(np.ones((2,2),np.float32),step,'unknown.geometry')


def test_area_downsample_retains_small_contributor_nearest_would_miss():
    source=np.zeros((4,4),np.float32);source[0,1]=1
    step=ResizeTransform(kind='resize',from_canvas=canvas(4,4,'before'),to_canvas=canvas(2,2,'after'),
        invertibility='partial',valid_region=Rect(x=0,y=0,width=2,height=2),pixel_centers='half_pixel')
    result=assemble(source,step,'derivative.resize')
    assert np.array_equal(result,np.array([[1,0],[0,0]],np.float32))


def test_area_downsample_exact_integer_boundary_excludes_other_box():
    source=np.zeros((4,4),np.float32);source[0,2]=1
    step=ResizeTransform(kind='resize',from_canvas=canvas(4,4,'before'),to_canvas=canvas(2,2,'after'),
        invertibility='partial',valid_region=Rect(x=0,y=0,width=2,height=2),pixel_centers='half_pixel')
    assert np.array_equal(assemble(source,step,'derivative.resize'),np.array([[0,1],[0,0]],np.float32))
