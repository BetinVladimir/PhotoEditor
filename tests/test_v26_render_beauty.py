import numpy as np
import pytest
from photo_worker.contracts.images import ColorSpace
from photo_worker.contracts.style import Beauty
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_beauty import prepare_beauty_stage
from test_v26_render_authored import fixture


def params():return Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=.2,tone_evenness=0,
    texture_keep=1,skin_mask='face68_conservative_v1',protect_features=True)


def test_existing_beauty_stage_preserves_no_face_review_and_owned_pixels(tmp_path):
    artifacts,inputs,_,_,_,path=fixture(tmp_path)
    inputs.source.color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='c'*64)
    current=AdjustmentPixels(np.full((1,2,3),.25,np.float32),None)
    try:
        with prepare_beauty_stage(artifacts,inputs.source,current,params(),()) as result:
            np.testing.assert_array_equal(result.pixels.rgb,current.rgb)
            assert result.review_required and result.faces==()
        assert set(tmp_path.rglob('*.npy'))=={path}
    finally:artifacts.scratch.cleanup()


def test_existing_working_color_guard_retained(tmp_path):
    artifacts,inputs,_,_,_,_=fixture(tmp_path)
    try:
        with pytest.raises(ValueError,match='beauty color'):
            with prepare_beauty_stage(artifacts,inputs.source,AdjustmentPixels(np.zeros((1,2,3),np.float32),None),params(),()):pass
    finally:artifacts.scratch.cleanup()


def test_authored_beauty_strength_uses_shared_existing_effective_semantics():
    from types import SimpleNamespace
    from photo_worker.v2.render_beauty import render_beauty_params
    authored=params();authored.tone_evenness=.3
    result=render_beauty_params(SimpleNamespace(effective=SimpleNamespace(beauty=authored,strength=.4)))
    assert result.smoothing==.2*.4 and result.tone_evenness==.3*.4
    assert result.texture_keep==1 and authored.smoothing==.2
