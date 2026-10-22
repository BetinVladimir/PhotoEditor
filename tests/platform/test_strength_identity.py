"""Fractional style strength must preserve unchanged/protected pixels."""
import numpy as np
import pytest
from photo_worker.algorithms.color import apply_strength,to_linear,to_encoded


@pytest.mark.parametrize('strength',[.01,.25,.5,.99])
def test_fractional_strength_keeps_equal_pixels_exact(strength):
    base=np.random.default_rng(151).random((17,19,3),dtype=np.float32)
    processed=base.copy();processed[3:8,4:9]=np.clip(processed[3:8,4:9]*.8,0,1)
    output=apply_strength(base,processed,strength)
    untouched=np.all(base==processed,axis=-1)
    np.testing.assert_array_equal(output[untouched],base[untouched])
    expected=to_encoded(to_linear(base)*(1-strength)+to_linear(processed)*strength)
    np.testing.assert_array_equal(output[~untouched],expected[~untouched])


@pytest.mark.parametrize('strength',[0.,.2,.7,1.])
def test_identity_strength_output_is_owned_and_does_not_mutate_source(strength):
    base=np.random.default_rng(152).random((13,17,3),dtype=np.float32);before=base.copy()
    result=apply_strength(base,base,strength)
    np.testing.assert_array_equal(result,base)
    result[0,0]=0
    np.testing.assert_array_equal(base,before)


def test_neutral_series_is_exact_owned_identity():
    from photo_worker.algorithms.color import apply_series
    rgb=np.random.default_rng(153).random((13,17,3),dtype=np.float32)
    output=apply_series(rgb,0,[1.,1.,1.])
    np.testing.assert_array_equal(output,rgb)
    assert not np.shares_memory(output,rgb)


@pytest.mark.parametrize('neutral',['both','person','background'])
def test_local_neutral_regions_preserve_exact_encoded_values(neutral):
    from photo_worker.algorithms.color import apply_local
    from photo_worker.contracts.style import LocalColor
    rgb=np.random.default_rng(154).random((13,17,3),dtype=np.float32)
    mask=np.zeros((13,17),np.float32);mask[:,8:]=1;mask[:,7]=.5
    config=LocalColor(enabled=True,person_ev=0 if neutral!='background' else .3,
        background_ev=0 if neutral!='person' else -.3,person_saturation=1,
        background_saturation=1,mask_policy_id='person_guided_v1')
    output=apply_local(rgb,mask,config)
    preserved=np.ones(mask.shape,bool) if neutral=='both' else mask==(1 if neutral=='person' else 0)
    np.testing.assert_array_equal(output[preserved],rgb[preserved])
    assert not np.shares_memory(output,rgb)


@pytest.mark.parametrize("encoding", ["constant", "dense"])
def test_neutral_global_colour_exact_identity_preserves_diagnostics(encoding):
    from photo_worker.algorithms.color import apply_global, ColorDiagnostics, KNOTS
    from photo_worker.contracts.style import GlobalColor, LUT
    rgb = np.random.default_rng(157).random((13, 17, 3), dtype=np.float32)
    config = GlobalColor(gains_rgb=[1, 1, 1], master_curve=KNOTS.tolist(),
        rgb_curves=[KNOTS.tolist() for _ in range(3)], saturation=1,
        residual_lut=LUT(kind="residual", size=17, encoding=encoding,
                         values=[0.] * (3 if encoding == "constant" else 17**3*3)))
    diagnostic = ColorDiagnostics()
    output = apply_global(rgb, config, diagnostic)
    np.testing.assert_array_equal(output, rgb)
    assert not np.shares_memory(output, rgb)
    assert set(diagnostic.clipping) == {"gains", "saturation", "residual_lut"}
    outside = rgb.copy(); outside[0, 0] = [-.1, .5, 1.1]
    np.testing.assert_array_equal(apply_global(outside, config), np.clip(outside, 0, 1))
