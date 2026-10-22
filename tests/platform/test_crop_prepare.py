from types import SimpleNamespace
from uuid import uuid4
import cv2
import numpy as np
import pytest
from photo_worker.algorithms.color import to_encoded, to_linear
from photo_worker.operations.geometry.prepare import prepare_crop
from photo_worker.runtime.scratch import ScratchSpace


@pytest.mark.parametrize('with_alpha', [False, True])
def test_crop_matches_original_linear_light_math_and_releases_files(tmp_path, with_alpha):
    rng = np.random.default_rng(81)
    rgb = rng.random((13, 17, 3), dtype=np.float32)
    alpha = np.full((13, 17), 65535, dtype=np.uint16) if with_alpha else None
    matrix = np.array([[.83, .01, -.5], [.02, .91, -.7], [0, 0, 1]], np.float64)
    plan = SimpleNamespace(source_height=13, source_width=17, output_height=11,
        output_width=14, disposition='apply', forward_matrix=matrix.ravel().tolist())
    expected = to_encoded(cv2.warpPerspective(to_linear(rgb), matrix, (14, 11),
        flags=cv2.INTER_LANCZOS4, borderMode=cv2.BORDER_REFLECT_101)).clip(0, 1)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1000000, min_free_bytes=0) as scratch:
        with prepare_crop(rgb, alpha, plan, scratch) as crop:
            path = crop.rgb
            np.testing.assert_array_equal(np.load(path), expected)
            if with_alpha:
                expected_alpha = np.rint(np.clip(cv2.warpPerspective(alpha.astype(np.float32),
                    matrix, (14, 11), flags=cv2.INTER_LANCZOS4,
                    borderMode=cv2.BORDER_REFLECT_101), 0, 65535)).astype(np.uint16)
                np.testing.assert_array_equal(np.load(crop.alpha), expected_alpha)
            else:
                assert crop.alpha is None
        assert not path.exists()


def test_crop_stage_preserves_geometry_and_review(tmp_path):
    from test_composition_crop import Artifacts, image, ref
    from photo_worker.contracts.composition import ImageStructure
    from photo_worker.operations.composition.plan import execute
    from test_asset_adapters import context_for
    from photo_worker.operations.geometry.prepare import prepare_crop_stage
    source = image(17, 13)
    artifacts = Artifacts(source, ImageStructure(image=ref()))
    execute(context_for(artifacts.structure_ref, 'composition.plan'), artifacts)
    plan = artifacts.staged[0][1]
    plan_ref = ref(plan)
    rgb = np.full((13, 17, 3), .4, np.float32)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1000000, min_free_bytes=0) as scratch:
        with prepare_crop_stage(artifacts.image_ref, source, plan_ref, plan, rgb,
                None, scratch) as stage:
            assert stage.canvas.source_sha256 == source.canvas.source_sha256
            assert stage.transform_chain.steps[-1].from_canvas == source.canvas
            assert stage.transform_chain.steps[-1].to_canvas == stage.canvas
            assert stage.rgb.shape == (plan.output_height, plan.output_width, 3)
            assert not stage.rgb.flags.writeable
            assert stage.review_required == (plan.disposition == 'review')
            assert stage.review_reasons == tuple(plan.reasons)
        with pytest.raises(ValueError, match='CROP_PLAN_SOURCE_INVALID'):
            with prepare_crop_stage(ref(), source, plan_ref, plan, rgb, None, scratch):
                pytest.fail('foreign source accepted')


def test_transparent_hidden_color_cannot_bleed_into_crop_edge(tmp_path):
    alpha=np.zeros((8,8),np.uint16);alpha[:, :4]=65535
    rgb=np.zeros((8,8,3),np.float32);rgb[:,:,0]=1
    hidden=rgb.copy();hidden[:,4:]=[0,1,0]
    matrix=[1,0,.5,0,1,0,0,0,1]
    plan=SimpleNamespace(source_height=8,source_width=8,output_height=8,
        output_width=8,disposition='apply',forward_matrix=matrix)
    outputs=[]
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        for source in (rgb,hidden):
            with prepare_crop(source,alpha,plan,scratch) as crop:
                outputs.append((np.load(crop.rgb).copy(),np.load(crop.alpha).copy()))
    np.testing.assert_array_equal(outputs[0][0],outputs[1][0])
    np.testing.assert_array_equal(outputs[0][1],outputs[1][1])
    visible=outputs[0][1]>0
    assert np.any((outputs[0][1]>0)&(outputs[0][1]<65535))
    assert np.all(outputs[0][0][visible,1:]==0)


def test_fully_transparent_resampled_crop_has_finite_zero_rgb(tmp_path):
    rgb=np.ones((4,4,3),np.float32);alpha=np.zeros((4,4),np.uint16)
    plan=SimpleNamespace(source_height=4,source_width=4,output_height=4,
        output_width=4,disposition='apply',forward_matrix=[1,0,.5,0,1,0,0,0,1])
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with prepare_crop(rgb,alpha,plan,scratch) as crop:
            output=np.load(crop.rgb)
            assert np.isfinite(output).all() and np.all(output==0)
            assert np.all(np.load(crop.alpha)==0)


@pytest.mark.parametrize('left,top,width,height',[(0,0,17,13),(3,2,11,8)])
@pytest.mark.parametrize('with_alpha',[False,True])
def test_integer_crop_preserves_exact_source_values(tmp_path,monkeypatch,left,top,width,height,with_alpha):
    rgb=np.random.default_rng(83).random((13,17,3),dtype=np.float32)
    alpha=np.random.default_rng(84).integers(0,65536,(13,17),dtype=np.uint16) if with_alpha else None
    if alpha is not None:alpha[:,::2]=0
    plan=SimpleNamespace(source_height=13,source_width=17,output_height=height,
        output_width=width,disposition='apply',forward_matrix=[1,0,-left,0,1,-top,0,0,1])
    def no_resampling(*args,**kwargs):raise AssertionError('integer crop must not resample')
    monkeypatch.setattr(cv2,'warpPerspective',no_resampling)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with prepare_crop(rgb,alpha,plan,scratch) as crop:
            np.testing.assert_array_equal(np.load(crop.rgb),rgb[top:top+height,left:left+width])
            if alpha is not None:
                np.testing.assert_array_equal(np.load(crop.alpha),alpha[top:top+height,left:left+width])


def test_integer_crop_cancellation_does_not_publish_partial_result(tmp_path):
    rgb=np.ones((600,4,3),np.float32)
    plan=SimpleNamespace(source_height=600,source_width=4,output_height=600,
        output_width=4,disposition='apply',forward_matrix=[1,0,0,0,1,0,0,0,1])
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==4:raise RuntimeError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_crop(rgb,None,plan,scratch,check_active=active):
                pytest.fail('partial integer crop was published')
        assert not list(tmp_path.rglob('*.npy'))
