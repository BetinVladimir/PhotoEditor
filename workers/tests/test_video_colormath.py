import numpy as np
import pytest

from photo_worker.video.colormath import cdl, delta_e76, exposure, pchip_curve, rgb_to_lab, srgb_to_linear
from video_vectors import load_vector

V = load_vector('color')


def test_srgb_to_linear():
    for c in V['srgb_to_linear']:
        assert srgb_to_linear(c['in']) == pytest.approx(c['out'], abs=1e-6)


def test_exposure_including_clamped_extremes():
    assert len(V['exposure']) == 6
    for c in V['exposure']:
        assert exposure(c['rgb'], c['ev']) == pytest.approx(c['out'], abs=1e-6)


def test_cdl():
    assert len(V['cdl']) == 7
    for c in V['cdl']:
        assert cdl(c['rgb'], c['slope'], c['offset'], c['power'], c['sat']) == pytest.approx(c['out'], abs=1e-6)


def test_lab_and_delta_e():
    for c in V['lab']:
        assert rgb_to_lab(c['rgb']) == pytest.approx(c['lab'], abs=1e-3)
    for c in V['delta_e76']:
        assert delta_e76(c['a'], c['b']) == pytest.approx(c['de'], abs=1e-3)


def test_pchip_vector_and_range_error():
    curve = pchip_curve(V['pchip']['points'])
    assert len(curve) == 4096
    for e in V['pchip']['eval']:
        assert float(np.interp(e['x'], np.linspace(0, 1, 4096), curve)) == pytest.approx(e['y'], abs=1e-6)
    with pytest.raises(ValueError, match='CURVE_RANGE'):
        pchip_curve([[0, 0], [1.5, 1]])
    with pytest.raises(ValueError, match='CURVE_RANGE'):
        pchip_curve([[0, 0], [0.5, 0.5], [0.5, 0.6]])
