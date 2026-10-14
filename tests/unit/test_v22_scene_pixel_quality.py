"""Controlled pixel regression. Does not replace real-photo/human V22 acceptance."""
from math import exp
import numpy as np
import pytest
from photo_worker.operations.session.plan import Frame, solve_consistency

LUMA = np.array([.2126, .7152, .0722], dtype=np.float64)


def descriptor(index, pixels, neutral=None):
    return Frame(index, None, None, (1.0,) + (0.0,) * 383,
                 float(np.median(pixels @ LUMA)), neutral, 10.0,
                 float(np.mean(np.any((pixels >= 1) | (pixels < 0), axis=-1))))


def apply(pixels, correction):
    # Independent execution of the documented EV then diagonal gain equation.
    gains = 2 ** correction.ev * np.array([
        exp(correction.log_r), exp(correction.log_g), exp(correction.log_b)])
    return pixels * gains


def residual(images):
    levels = np.array([np.median(image @ LUMA) for image in images])
    return float(np.median(np.abs(np.log2(levels / levels[0]))))


def clipping(images):
    return float(np.mean([np.mean(np.any((image >= 1) | (image < 0), axis=-1))
                          for image in images]))


def test_controlled_scene_measures_residual_and_clipping_on_pixels():
    # Different skin-like and background colors remain spatially distinct.
    base = np.array([[[.42, .25, .16], [.12, .24, .32]],
                     [[.31, .19, .13], [.18, .22, .14]]], dtype=np.float64)
    originals = [base * scale for scale in (1, .55, .7, .85, 1.15)]
    corrections = solve_consistency([descriptor(i, image) for i, image in enumerate(originals)], 0)
    corrected = [apply(image, correction) for image, correction in zip(originals, corrections, strict=True)]
    before, after = residual(originals), residual(corrected)
    assert before > 0
    assert (before - after) / before >= .20
    assert clipping(corrected) - clipping(originals) <= .005
    for image, result, correction in zip(originals, corrected, corrections, strict=True):
        assert correction.saturation_delta == correction.retouch_delta == 0
        assert np.allclose(result / result.sum(axis=-1, keepdims=True),
                           image / image.sum(axis=-1, keepdims=True), atol=1e-12)
        assert np.isfinite(result).all()


def test_manual_locked_frame_is_pixel_exact_and_not_counted_as_corrected():
    image = np.array([[[.4, .2, .1], [.8, .7, .6]]])
    frames = [descriptor(0, image), descriptor(1, image * .5)]
    correction = solve_consistency(frames, 0, frozenset({1}))[1]
    assert correction.disposition == 'manual_locked'
    assert np.array_equal(apply(image, correction), image)


def test_quality_gate_can_detect_clipping_regression_despite_lower_ev_residual():
    # A bright minority clips even when median luma aligns: EV alone cannot prove safety.
    reference = np.full((20, 20, 3), .4)
    target = np.full((20, 20, 3), .2)
    target[0] = .8
    correction = solve_consistency([descriptor(0, reference), descriptor(1, target)], 0)[1]
    corrected = apply(target, correction)
    assert residual([reference, corrected]) < residual([reference, target])
    assert clipping([reference, corrected]) - clipping([reference, target]) > .005


@pytest.mark.parametrize('value', [0.0, 1e-8])
def test_invalid_luminance_does_not_invent_a_correction(value):
    image = np.full((2, 2, 3), value)
    reference = np.full_like(image, .4)
    correction = solve_consistency([descriptor(0, reference), descriptor(1, image)], 0)[1]
    assert correction.reason == 'INVALID_LUMINANCE'
    assert np.array_equal(apply(image, correction), image)
