"""Authored geometry projection scenarios, independent of image resampling."""
import numpy as np
import pytest
from photo_worker.contracts.images import Canvas,Rect,CropTransform,ResizeTransform,DenseWarpTransform,MatrixTransform
from photo_worker.operations.region.projection import ProjectedBounds,project_region_bounds


def canvas(w,h):return Canvas(width=w,height=h,orientation=1,space_id='work',source_sha256='a'*64)
def common(sw,sh,w,h):return dict(from_canvas=canvas(sw,sh),to_canvas=canvas(w,h),invertibility='partial',valid_region=Rect(x=0,y=0,width=w,height=h))


def test_crop_then_upscale_projects_to_processed_pixels_without_resizeback():
    bounds=ProjectedBounds(2,2,4,3)
    crop=CropTransform(kind='crop',crop_rect=Rect(x=1,y=1,width=7,height=6),**common(10,8,7,6))
    bounds=project_region_bounds(bounds,crop)
    assert bounds==ProjectedBounds(1,1,4,3)
    resize=ResizeTransform(kind='resize',pixel_centers='half_pixel',**common(7,6,14,12))
    assert project_region_bounds(bounds,resize)==ProjectedBounds(2,2,8,6)


def test_crop_removed_roi_is_explicit_unavailable_not_wrong_region():
    crop=CropTransform(kind='crop',crop_rect=Rect(x=5,y=0,width=5,height=8),**common(10,8,5,8))
    with pytest.raises(ValueError,match='REGION_OUTSIDE_RENDERED_CANVAS'):
        project_region_bounds(ProjectedBounds(0,0,2,2),crop)


def test_outpaint_translation_keeps_original_roi_not_new_border():
    step=MatrixTransform(kind='matrix',forward_matrix=[1,0,2,0,1,3,0,0,1],**common(10,8,14,14))
    assert project_region_bounds(ProjectedBounds(1,1,3,2),step)==ProjectedBounds(3,4,3,2)


def test_dense_projection_uses_nonzero_contributors_not_nearest():
    from uuid import UUID
    from photo_worker.contracts.base import ArtifactRef
    step=DenseWarpTransform(kind='dense_warp',inverse_field=ArtifactRef(artifact_id=UUID(int=1),sha256='b'*64),**common(4,4,4,4))
    field=np.zeros((4,4,2),np.float32);field[...,0]=0;field[...,1]=0
    field[2,3]=[.25,.25]
    assert project_region_bounds(ProjectedBounds(1,1,1,1),step,inverse_field=field)==ProjectedBounds(3,2,1,1)


def test_chain_requires_append_only_actual_source_history():
    from photo_worker.contracts.images import TransformChain
    from photo_worker.operations.region.projection import project_region_chain
    crop=CropTransform(kind='crop',crop_rect=Rect(x=1,y=1,width=7,height=6),**common(10,8,7,6))
    resize=ResizeTransform(kind='resize',pixel_centers='half_pixel',**common(7,6,14,12))
    def unexpected(*args):raise AssertionError('No dense field needed')
    result=project_region_chain(ProjectedBounds(1,1,4,3),crop.to_canvas,resize.to_canvas,
        TransformChain.create([crop]),TransformChain.create([crop,resize]),load_inverse_field=unexpected)
    assert result==ProjectedBounds(2,2,8,6)
    with pytest.raises(ValueError,match='REGION_PROJECTION_CHAIN_REPLACED'):
        project_region_chain(ProjectedBounds(1,1,4,3),crop.to_canvas,resize.to_canvas,
            TransformChain.create([crop]),TransformChain.create([resize]),load_inverse_field=unexpected)


def test_dense_projection_observes_lease_between_bounded_tiles():
    from uuid import UUID
    from photo_worker.contracts.base import ArtifactRef
    step=DenseWarpTransform(kind='dense_warp',inverse_field=ArtifactRef(artifact_id=UUID(int=1),sha256='b'*64),**common(4,4,4,4))
    field=np.zeros((4,4,2),np.float32)
    calls=[]
    def active():
        calls.append(1)
        if len(calls)==2:raise RuntimeError('lease lost')
    with pytest.raises(RuntimeError,match='lease lost'):
        project_region_bounds(ProjectedBounds(0,0,1,1),step,inverse_field=field,check_active=active)


def test_dense_wide_panorama_projection_keeps_horizontal_tile_offsets():
    from uuid import UUID
    from photo_worker.contracts.base import ArtifactRef
    step=DenseWarpTransform(kind='dense_warp',inverse_field=ArtifactRef(artifact_id=UUID(int=1),sha256='b'*64),**common(4,4,1025,2))
    field=np.zeros((2,1025,2),np.float32)
    field[1,513]=[.25,.25];field[0,1024]=[.5,.5]
    assert project_region_bounds(ProjectedBounds(1,1,1,1),step,inverse_field=field)==ProjectedBounds(513,0,512,2)


def test_dense_projection_observes_cancel_between_horizontal_tiles():
    from uuid import UUID
    from photo_worker.contracts.base import ArtifactRef
    step=DenseWarpTransform(kind='dense_warp',inverse_field=ArtifactRef(artifact_id=UUID(int=1),sha256='b'*64),**common(4,4,1025,1))
    field=np.zeros((1,1025,2),np.float32);calls=0
    def active():
        nonlocal calls
        calls+=1
        # Initial check + first tile + second horizontal tile.
        if calls==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        project_region_bounds(ProjectedBounds(1,1,1,1),step,inverse_field=field,check_active=active)
    assert calls==3
