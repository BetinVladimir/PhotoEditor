from uuid import UUID

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.series import RegionStats, SeriesParams, SeriesStats
from photo_worker.operations.series.balance import compute_series_corrections


def _series():
    refs = [ArtifactRef(artifact_id=UUID(int=10 + index), sha256=f'{index + 1:064x}')
            for index in range(2)]
    region = RegionStats(region_id='sky', mean_rgb=[.5, .5, .5],
                         mean_y=.5, coverage=.4, confidence=.9)
    values = [SeriesStats(item_id=UUID(int=index + 1), image=refs[index],
                          regions=[region], clipping_fraction=.01, warnings=[])
              for index in range(2)]
    return SeriesParams(scene_id='studio', stats=refs), values


def test_series_medoid_stops_after_lease_loss():
    params, values = _series()
    calls = 0

    def check_active():
        nonlocal calls
        calls += 1
        if calls == 2:
            raise RuntimeError('lease lost')

    with pytest.raises(RuntimeError, match='lease lost'):
        compute_series_corrections(params, values, check_active)
    assert calls == 2


def test_series_fit_stops_after_lease_loss_without_changing_result():
    params, values = _series()
    params = params.model_copy(update={'reference_item_id': values[0].item_id})
    expected = compute_series_corrections(params, values)
    assert canonical_bytes(compute_series_corrections(
        params, values, lambda: None)) == canonical_bytes(expected)
    calls = 0

    def check_active():
        nonlocal calls
        calls += 1
        if calls == 4:
            raise RuntimeError('lease lost')

    with pytest.raises(RuntimeError, match='lease lost'):
        compute_series_corrections(params, values, check_active)
    assert calls == 4
