from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.learning.person_stats import (
    open_holdout_person_stats,
)
from photo_worker.runtime.scratch import ScratchSpace

from test_image_color_adapter import Artifacts


class Pool:
    lock = SimpleNamespace(lock_id='a' * 64,
                           models=[SimpleNamespace(id='lraspp_mobilenet_v3_large')])

    @contextmanager
    def acquire(self, model_id, backend):
        assert model_id == 'lraspp_mobilenet_v3_large' and backend == 'cuda'
        yield SimpleNamespace(infer=lambda value: value)


def test_holdout_person_stats_reuses_exact_statistics(tmp_path, monkeypatch):
    artifacts = Artifacts(tmp_path, False)
    monkeypatch.setattr(
        'photo_worker.operations.learning.person_stats._preview',
        lambda rgb, long_side: rgb)
    monkeypatch.setattr(
        'photo_worker.operations.learning.person_stats._probability',
        lambda logits, height, width:
        (np.full((height, width), .7, np.float32), .9))
    root = tmp_path / 'scratch'
    root.mkdir()
    alpha = np.full(artifacts.rgb.shape[:2], 65535, np.uint16)
    with ScratchSpace(root, uuid4(), uuid4(), 1, 500_000,
                      min_free_bytes=0) as scratch:
        with open_holdout_person_stats(artifacts.rgb, alpha, Pool(),
                                       'cuda', 'a' * 64, scratch,
                                       lambda: None) as result:
            assert result.mask.shape == artifacts.rgb.shape[:2]
            assert result.features.shape == (20,)
            assert result.coverage_counts.sum() == artifacts.rgb.shape[0] * artifacts.rgb.shape[1]
            assert result.confidence == .9 and result.reliable
        assert list(scratch.directory.iterdir()) == []


def test_holdout_person_stats_rejects_changed_model_lock(tmp_path):
    artifacts = Artifacts(tmp_path, False)
    root = tmp_path / 'scratch'
    root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 500_000,
                      min_free_bytes=0) as scratch:
        with pytest.raises(ValueError, match='HOLDOUT_PERSON_INPUT_INVALID'):
            with open_holdout_person_stats(artifacts.rgb, None, Pool(),
                                           'cuda', '0' * 64, scratch,
                                           lambda: None):
                pass
