import numpy as np
import pytest
from photo_worker.algorithms.portrait_donor import sample_donor_pixels

IDENTITY = np.array([[1., 0., 0.], [0., 1., 0.]])


def test_alpha_sampling_excludes_hidden_rgb_and_returns_straight_color():
    donor=np.array([[[100.,50.,20.],[2.,3.,4.]]],dtype=np.float32)
    alpha=np.array([[0.,1.]],dtype=np.float32)
    rgb,opacity=sample_donor_pixels(donor,IDENTITY,[[.5,0],[0,0]],
                                    donor_alpha=alpha,return_alpha=True)
    np.testing.assert_array_equal(rgb,[[2,3,4],[0,0,0]])
    np.testing.assert_array_equal(opacity,[.5,0])
    np.testing.assert_array_equal(alpha,[[0,1]])


@pytest.mark.parametrize('value',[np.nan,-.1,1.1])
def test_invalid_sampled_alpha_rejected(value):
    with pytest.raises(ValueError,match='DONOR_ALPHA'):
        sample_donor_pixels(np.ones((1,1,3),np.float32),IDENTITY,[[0,0]],
                            donor_alpha=np.array([[value]],np.float32))


def test_gamma_sampling_decodes_before_interpolation_without_hdr_clipping():
    donor=np.array([[[0.,0.,0.],[2.,2.,2.]]],dtype=np.float32)
    original=donor.copy()
    result=sample_donor_pixels(donor,IDENTITY,[[.5,0]],encoding='gamma24')
    np.testing.assert_allclose(result, np.full((1,3),2**2.4/2),rtol=1e-6)
    assert result.min()>1
    np.testing.assert_array_equal(donor,original)


def test_gamma_overflow_and_unknown_encoding_fail():
    donor=np.full((1,1,3),np.finfo(np.float32).max,dtype=np.float32)
    with pytest.raises(ValueError,match='DONOR_PIXELS'):
        sample_donor_pixels(donor,IDENTITY,[[0,0]],encoding='gamma24')
    with pytest.raises(ValueError,match='DONOR_GEOMETRY'):
        sample_donor_pixels(donor,IDENTITY,[[0,0]],encoding='srgb')


def test_bilinear_sampling_identity_edges_fractional_and_hdr():
    donor = np.arange(27, dtype=np.float32).reshape(3, 3, 3)
    original = donor.copy()
    points = np.array([[0, 0], [2, 2], [.5, .5]])
    result = sample_donor_pixels(donor, IDENTITY, points)
    np.testing.assert_array_equal(result[:2], donor[[0, 2], [0, 2]])
    np.testing.assert_array_equal(result[2], donor[:2, :2].mean(axis=(0, 1)))
    assert result.max() > 1
    np.testing.assert_array_equal(donor, original)


def test_inverse_transform_samples_correct_donor_location():
    donor = np.arange(27, dtype=np.float32).reshape(3, 3, 3)
    transform = np.array([[1.1, 0., 7.], [0., 1.1, -3.]])
    result = sample_donor_pixels(donor, transform, [[8.1, -1.9]])
    np.testing.assert_allclose(result[0], donor[1, 1], atol=1e-6)


@pytest.mark.parametrize('point', [[-.001, 1], [1, -.001], [2.001, 1], [1, 2.001]])
def test_missing_coverage_never_uses_border_padding(point):
    with pytest.raises(ValueError, match='DONOR_COVERAGE'):
        sample_donor_pixels(np.ones((3, 3, 3), np.float32), IDENTITY, [point])


def test_invalid_sampled_pixels_and_reflection_fail():
    donor = np.ones((3, 3, 3), np.float32)
    donor[1, 1, 0] = np.nan
    with pytest.raises(ValueError, match='DONOR_PIXELS'):
        sample_donor_pixels(donor, IDENTITY, [[1, 1]])
    with pytest.raises(ValueError, match='DONOR_GEOMETRY'):
        sample_donor_pixels(donor, [[-1, 0, 2], [0, 1, 0]], [[1, 1]])
