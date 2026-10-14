from uuid import uuid4

import pytest
from pydantic import ValidationError

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.series import RegionStats, SeriesStats


def _region(region_id='sky', rgb=None):
    return RegionStats(region_id=region_id,
        mean_rgb=[.2, .3, .4] if rgb is None else rgb,
        mean_y=.3, coverage=.5, confidence=.9)


def _stats(**changes):
    values = dict(item_id=uuid4(),
        image=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64),
        regions=[_region()], clipping_fraction=.01, warnings=[])
    values.update(changes)
    return SeriesStats(**values)


def test_series_stats_region_ids_and_means_are_unambiguous():
    assert _stats().regions[0].mean_rgb == [.2, .3, .4]
    for rgb in ([-.1, .3, .4], [1.1, .3, .4],
                [float('nan'), .3, .4], [float('inf'), .3, .4]):
        with pytest.raises(ValidationError):
            _region(rgb=rgb)
    with pytest.raises(ValidationError):
        _region(region_id='invalid region')
    with pytest.raises(ValidationError):
        _stats(regions=[_region(), _region()])
    with pytest.raises(ValidationError):
        _stats(warnings=['low_mask_confidence', 'low_mask_confidence'])
