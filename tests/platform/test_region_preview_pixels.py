import numpy as np
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.export.encode import _resize_rgba
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.pixels import extract_rendered_region
from test_region_preview_contract import region_fixture


def fixture():
    plan,image=region_fixture()
    plan['request']['rect']=dict(x0=.25,y0=.25,x1=.75,y1=.75)
    plan['request']['max_edge']=128
    plan['geometry']=dict(left=100,top=75,width=200,height=150,output_width=128,output_height=96)
    return RegionPreviewParams.model_validate_json(canonical_bytes(plan)),canonical_bytes(image)


def test_extract_region_matches_existing_resizer_and_owns_output():
    params,body=fixture()
    rgb=np.random.default_rng(2).random((300,400,3),dtype=np.float32)
    expected,_=_resize_rgba(rgb[75:225,100:300],None,128,96,encoding='gamma24')
    out,alpha=extract_rendered_region(params,body,rgb)
    assert out.shape==(96,128,3) and alpha is None
    assert np.array_equal(out,expected)
    assert not np.shares_memory(out,rgb)
    assert out.flags.c_contiguous


def test_no_resize_still_detaches_from_full_frame():
    plan,image=region_fixture()
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    rgb=np.full((300,400,3),.5,np.float32)
    out,_=extract_rendered_region(params,canonical_bytes(image),rgb)
    assert np.array_equal(out,rgb) and not np.shares_memory(out,rgb)


@pytest.mark.parametrize('rgb',[
    np.zeros((150,200,3),np.float32),np.zeros((300,400,3),np.uint16),
    np.full((300,400,3),np.nan,np.float32),np.full((300,400,3),1.01,np.float32),
])
def test_partial_canvas_or_invalid_values_are_rejected(rgb):
    params,body=fixture()
    with pytest.raises(ValueError,match='REGION_RENDER_INVALID'):extract_rendered_region(params,body,rgb)


def test_cancel_observed_before_scanning_or_resize():
    params,body=fixture()
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        extract_rendered_region(params,body,np.zeros((300,400,3),np.float32),check_active=cancel)


def test_linear_alpha_resize_preserves_visible_color_and_opacity():
    rgb=np.array([[[1.,.5,.25],[0.,0.,0.]]],np.float32)
    alpha=np.array([[1.,0.]],np.float32)
    out,opacity=_resize_rgba(rgb,alpha,1,1,encoding='linear')
    assert np.array_equal(out,np.array([[[1.,.5,.25]]],np.float32))
    assert np.array_equal(opacity,np.array([[.5]],np.float32))


def test_invalid_pixels_outside_requested_roi_are_rejected():
    params,body=fixture()
    rgb=np.zeros((300,400,3),np.float32)
    rgb[0,0,0]=np.nan
    with pytest.raises(ValueError,match='REGION_RENDER_INVALID'):
        extract_rendered_region(params,body,rgb)


def test_projected_upscale_extracts_actual_processed_canvas_without_resizeback():
    from photo_worker.contracts.images import WorkingImage,ResizeTransform,TransformChain,Rect
    from photo_worker.operations.region.pixels import extract_projected_rendered_region
    plan,document=region_fixture();params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    source=WorkingImage.model_validate_json(canonical_bytes(document))
    target=source.canvas.model_copy(update=dict(width=800,height=600,space_id='upscaled'))
    step=ResizeTransform(kind='resize',from_canvas=source.canvas,to_canvas=target,invertibility='invertible',
        valid_region=Rect(x=0,y=0,width=800,height=600),pixel_centers='half_pixel')
    chain=TransformChain.create([*source.transform_chain.steps,step])
    from photo_worker.operations.region.execution import RegionExecutionDescriptor,RegionGeometryStage
    from photo_worker.contracts.base import ArtifactRef,canonical_hash
    from uuid import uuid4
    params.execution=RegionExecutionDescriptor(schema_version=1,source=params.source.ref,style=params.frozen_recipe.base_style,
        source_canvas_hash=canonical_hash(source.canvas),stages=[RegionGeometryStage(operation='image.upscale',operation_version=1,
        task_id=uuid4(),result_id=uuid4(),attempt=1,fencing_token=1,params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),
        source=params.source.ref,output=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),source_canvas=source.canvas,target_canvas=target,
        transform=step,target_has_alpha=False)])
    rendered=np.random.default_rng(27).random((600,800,3),dtype=np.float32)
    def field(*args):raise AssertionError('Resize has no dense field')
    rgb,alpha,geometry=extract_projected_rendered_region(params,canonical_bytes(document),rendered,None,
        target_canvas=target,target_chain=chain,load_inverse_field=field)
    assert geometry.width==800 and geometry.height==600
    assert geometry.output_width==800 and geometry.output_height==600
    assert np.array_equal(rgb,rendered) and not np.shares_memory(rgb,rendered)
    assert alpha is None


def test_projected_extraction_rejects_target_shape_substitution():
    from photo_worker.contracts.images import WorkingImage,ResizeTransform,TransformChain,Rect
    from photo_worker.operations.region.pixels import extract_projected_rendered_region
    plan,document=region_fixture();params=RegionPreviewParams.model_validate_json(canonical_bytes(plan));source=WorkingImage.model_validate_json(canonical_bytes(document))
    target=source.canvas.model_copy(update=dict(width=800,height=600,space_id='upscaled'))
    step=ResizeTransform(kind='resize',from_canvas=source.canvas,to_canvas=target,invertibility='invertible',
        valid_region=Rect(x=0,y=0,width=800,height=600),pixel_centers='half_pixel')
    with pytest.raises(ValueError,match='REGION_RENDER_INVALID'):
        extract_projected_rendered_region(params,canonical_bytes(document),np.zeros((300,400,3),np.float32),None,
            target_canvas=target,target_chain=TransformChain.create([*source.transform_chain.steps,step]),load_inverse_field=lambda *args:None)
