"""A cancelled capture proxy cannot start SFD/FAN observations."""

from types import SimpleNamespace
import numpy as np
import pytest

from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.capture import prepare as module
from photo_worker.operations.session import describe


def test_cancellation_after_model_preview_blocks_face_inference(monkeypatch):
    cancelled = False
    def active():
        if cancelled:
            raise RuntimeError('capture cancelled')
    def preview(*_):
        nonlocal cancelled
        cancelled = True
        return np.zeros((1, 1, 3), np.float32)
    monkeypatch.setattr(module, 'bind_proxy', lambda *_: None)
    monkeypatch.setattr(module, 'capture_model_preview', preview)
    monkeypatch.setattr(describe, '_face_observations', lambda *_: pytest.fail('inference after cancel'))
    source = SimpleNamespace(canvas={'source': 'pinned'}, pixels=object(), alpha=None)
    binding = SimpleNamespace(canvas_hash=canonical_hash(source.canvas))
    calibration = SimpleNamespace(qualified=True, model_lock_sha='a' * 64, min_face_edge_px=8)
    models = [SimpleNamespace(id=name, approved_for_install=True,
              backends={'cpu': SimpleNamespace(status='passed')}) for name in ('sfd', 'fan2d')]
    pool = SimpleNamespace(lock=SimpleNamespace(lock_id='a' * 64, models=models))
    prepared = SimpleNamespace(rgb=np.zeros((1, 1, 3), np.float32), alpha=None)
    with pytest.raises(RuntimeError, match='capture cancelled'):
        module.prepare_face_observations(source, prepared, binding, calibration,
                                         pool, 'cpu', active)
