import pytest
from pydantic import ValidationError
from photo_worker.contracts.images import Canvas,Rect,CropTransform,ResizeTransform,MatrixTransform,TransformChain,RelativeDepth,WorkingImage,ColorSpace
from photo_worker.contracts.base import ArtifactRef
from uuid import uuid4


def canvas(w=100,h=80):
    return Canvas(width=w,height=h,orientation=1,space_id='work_rec2020_gamma24_v1',source_sha256='a'*64)


def test_transform_chain_hash_continuity_and_target():
    original=canvas();cropped=canvas(50,40);small=canvas(25,20)
    crop=CropTransform(kind='crop',from_canvas=original,to_canvas=cropped,invertibility='partial',valid_region=Rect(x=0,y=0,width=50,height=40),crop_rect=Rect(x=10,y=10,width=50,height=40))
    resize=ResizeTransform(kind='resize',from_canvas=cropped,to_canvas=small,invertibility='partial',valid_region=Rect(x=0,y=0,width=25,height=20),pixel_centers='half_pixel')
    chain=TransformChain.create([crop,resize])
    assert chain.validate_target(small)==chain
    with pytest.raises(ValueError,match='target'):chain.validate_target(original)
    with pytest.raises(ValidationError,match='Discontinuous'):TransformChain.create([resize,crop])
    payload=chain.model_dump(mode='python');payload['hash']='b'*64
    with pytest.raises(ValidationError,match='hash mismatch'):TransformChain.model_validate(payload)


def test_invalid_warp_matrix_and_out_of_bounds_crop():
    common=dict(kind='matrix',from_canvas=canvas(),to_canvas=canvas(),invertibility='invertible',valid_region=Rect(x=0,y=0,width=100,height=80))
    with pytest.raises(ValidationError,match='Singular'):MatrixTransform(**common,forward_matrix=[0.]*9)
    with pytest.raises(ValidationError,match='horizon'):MatrixTransform(**common,forward_matrix=[1.,0.,0.,0.,1.,0.,1.,0.,-50.])
    with pytest.raises(ValidationError,match='exceeds canvas'):CropTransform(kind='crop',from_canvas=canvas(),to_canvas=canvas(50,40),invertibility='partial',valid_region=Rect(x=0,y=0,width=50,height=40),crop_rect=Rect(x=90,y=0,width=50,height=40))


def test_depth_shape_and_working_profile():
    ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    common=dict(artifact=ref,invalid_mask=None,canvas=canvas(),transform_chain=TransformChain.create([]),normalization='minmax_valid')
    assert RelativeDepth(**common,float32_shape=[80,100]).normalization=='minmax_valid'
    with pytest.raises(ValidationError,match='dimensions'):RelativeDepth(**common,float32_shape=[100,80])
    with pytest.raises(ValidationError,match='working profile'):WorkingImage(pixels=ref,canvas=canvas(),color=ColorSpace(profile_id='srgb_v1',icc_sha256='c'*64,encoding='linear'),dtype='float32',alpha=None,transform_chain=TransformChain.create([]))


def test_large_translation_remains_well_defined():
    c=canvas(100000,100)
    t=MatrixTransform(kind='matrix',from_canvas=c,to_canvas=c,invertibility='partial',valid_region=Rect(x=0,y=0,width=100000,height=100),forward_matrix=[1.,0.,50000.,0.,1.,0.,0.,0.,1.])
    assert t.forward_matrix[2]==50000.
