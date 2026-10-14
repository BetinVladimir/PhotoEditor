"""Geometry-master quality, tiled equality and HDR/alpha ownership; NOT RUN."""
import numpy as np
import pytest
from photo_worker.operations.quality.check import _technical, _decision
from photo_worker.operations.quality.working_metrics import measure_working_metrics


@pytest.mark.parametrize('shape', [(1,1), (1,33), (35,1), (31,47), (64,65)])
def test_tiled_laplacian_matches_whole_image_across_boundaries(shape):
    rgb = np.random.default_rng(7).random((*shape,3), dtype=np.float32)
    expected = _technical(rgb, [])
    actual = measure_working_metrics(rgb, tile_edge=16)
    assert actual.sharpness_laplacian == pytest.approx(expected.sharpness_laplacian, rel=2e-6, abs=1e-8)
    assert actual.clipped_black_fraction == expected.clipped_black_fraction
    assert actual.clipped_white_fraction == expected.clipped_white_fraction


def test_float_opacity_hides_transparent_border_without_mutating_hdr():
    rgb = np.full((32,32,3), .5, dtype=np.float32)
    rgb[0] = -2.; rgb[-1] = 4.
    alpha = np.ones((32,32), dtype=np.float32);alpha[0]=alpha[-1]=0
    original = rgb.copy()
    result = measure_working_metrics(rgb, alpha, tile_edge=16)
    assert result.below_zero_fraction == result.above_one_fraction == 1/32
    assert result.clipped_black_fraction == result.clipped_white_fraction == 0
    assert np.array_equal(rgb, original)
    assert _decision(result, None)[0] != 'fail'


@pytest.mark.parametrize('alpha_dtype', [np.float32,np.uint16])
def test_empty_coverage_uses_zero_visible_clipping(alpha_dtype):
    result = measure_working_metrics(np.zeros((17,19,3),np.float32),np.zeros((17,19),alpha_dtype))
    assert result.clipped_black_fraction == result.clipped_white_fraction == 0


def test_invalid_alpha_nonfinite_rgb_and_cancellation_are_refused():
    rgb = np.ones((17,19,3),np.float32)*.5
    alpha=np.ones((17,19),np.float32);alpha[0,0]=1.01
    with pytest.raises(ValueError,match='ALPHA_RANGE'):measure_working_metrics(rgb,alpha)
    rgb[0,0,0]=np.nan
    with pytest.raises(ValueError,match='NONFINITE'):measure_working_metrics(rgb)
    def stop():raise InterruptedError('stopped')
    with pytest.raises(InterruptedError):measure_working_metrics(np.zeros((17,19,3),np.float32),check_active=stop)


def test_measurement_temporaries_stay_tiled_for_a_wide_image(monkeypatch):
    import photo_worker.operations.quality.working_metrics as module
    observed=[]
    original=module.cv2.Laplacian
    def laplacian(gray,*args,**kwargs):
        observed.append(gray.shape)
        return original(gray,*args,**kwargs)
    monkeypatch.setattr(module.cv2,'Laplacian',laplacian)
    ticks=[]
    measure_working_metrics(np.full((33,8193,3),.5,np.float32),
        check_active=lambda:ticks.append(True),tile_edge=256)
    assert len(observed)>1 and max(h*w for h,w in observed)<=258*258
    assert len(ticks)>len(observed)
