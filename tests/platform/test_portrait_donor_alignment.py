import numpy as np
import pytest
from photo_worker.algorithms.portrait_donor import align_local_patch


def landmarks():
    return np.random.default_rng(23).uniform(20, 100, (68, 2))


def test_umeyama_recovers_documented_similarity_without_mutation():
    donor = landmarks()
    original = donor.copy()
    angle = np.deg2rad(10)
    rotation = np.array([[np.cos(angle), -np.sin(angle)], [np.sin(angle), np.cos(angle)]])
    source = donor @ (1.1 * rotation).T + [8, -4]
    result = align_local_patch(source, donor, 150)
    np.testing.assert_allclose(result.matrix[:, :2], 1.1 * rotation, atol=1e-12)
    np.testing.assert_allclose(result.matrix[:, 2], [8, -4], atol=1e-12)
    assert result.median_residual < 1e-10
    assert not result.matrix.flags.writeable
    np.testing.assert_array_equal(donor, original)


@pytest.mark.parametrize('mode', ['scale', 'rotation', 'reflection', 'outlier', 'degenerate', 'nan', 'shape'])
def test_incompatible_geometry_is_rejected(mode):
    donor = landmarks()
    source = donor.copy()
    if mode == 'scale': source *= 1.5
    elif mode == 'rotation': source = source @ np.array([[0., -1.], [1., 0.]])
    elif mode == 'reflection': source[:, 0] *= -1
    elif mode == 'outlier': source[27] += 100
    elif mode == 'degenerate': donor[:] = 1
    elif mode == 'nan': donor[0, 0] = np.nan
    else: donor = donor[:67]
    with pytest.raises(ValueError, match='DONOR_'):
        align_local_patch(source, donor, 150)
