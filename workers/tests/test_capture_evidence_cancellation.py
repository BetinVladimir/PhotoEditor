"""Capture evidence cannot publish a final face after model-time cancellation."""

from contextlib import contextmanager
from types import SimpleNamespace
import numpy as np
import pytest

from photo_worker.operations.capture import evidence as module


class Context:
    cancelled = False

    def check_active(self):
        if self.cancelled:
            raise RuntimeError('task cancelled during inference')


@pytest.mark.parametrize('family', ['eye_open', 'glare_mask'])
def test_last_face_inference_cancellation_refuses_evidence(monkeypatch, family):
    context = Context()
    monkeypatch.setattr(module, 'capture_model_preview', lambda *_: np.zeros((100, 100, 3), np.float32))
    points = [SimpleNamespace(x=.2, y=.3) for _ in range(68)]
    for start in (36, 42):
        for index in range(6):
            points[start + index] = SimpleNamespace(x=.2 + index * .04, y=.3)
    face = SimpleNamespace(box=SimpleNamespace(x0=.1, y0=.1, x1=.9, y1=.9),
                           quality=1, landmarks=points, face_id='face-1')

    class Engine:
        def infer(self, crop):
            context.cancelled = True
            return np.array([.8, .2], np.float32) if family == 'eye_open' else np.zeros(crop.shape[:2], np.float32)

    class Pool:
        lock = SimpleNamespace(models=[SimpleNamespace(id=family, approved_for_install=True,
            backends={'cpu': SimpleNamespace(status='passed')})])

        @contextmanager
        def acquire(self, model_id, backend):
            assert model_id == family and backend == 'cpu'
            yield Engine()

    with pytest.raises(RuntimeError, match='task cancelled during inference'):
        module.infer_face_evidence(context, object(), SimpleNamespace(faces=[face]),
            SimpleNamespace(min_face_edge_px=8), Pool(), 'cpu',
            SimpleNamespace(checks=['eyes' if family == 'eye_open' else 'glare']))


def test_no_model_path_still_checks_cancellation():
    context = Context()
    context.cancelled = True
    pool = SimpleNamespace(lock=SimpleNamespace(models=[]))
    with pytest.raises(RuntimeError, match='task cancelled during inference'):
        module.infer_face_evidence(context, object(), SimpleNamespace(faces=[]),
            object(), pool, 'cpu', SimpleNamespace(checks=[]))


def test_preview_cancellation_refuses_before_face_evidence(monkeypatch):
    context = Context()
    def preview(*_):
        context.cancelled = True
        return np.zeros((10, 10, 3), np.float32)
    monkeypatch.setattr(module, 'capture_model_preview', preview)
    model = SimpleNamespace(id='eye_open', approved_for_install=True,
                            backends={'cpu': SimpleNamespace(status='passed')})
    pool = SimpleNamespace(lock=SimpleNamespace(models=[model]))
    with pytest.raises(RuntimeError, match='task cancelled during inference'):
        module.infer_face_evidence(context, object(), SimpleNamespace(faces=[]),
            object(), pool, 'cpu', SimpleNamespace(checks=['eyes']))


@pytest.mark.parametrize('bad_value', [np.nan, np.inf, -0.1, 1.1])
def test_glare_invalid_model_mask_cannot_become_clean_evidence(monkeypatch, bad_value):
    monkeypatch.setattr(module, 'capture_model_preview',
                        lambda *_: np.zeros((10, 10, 3), np.float32))
    face = SimpleNamespace(box=SimpleNamespace(x0=.1, y0=.1, x1=.9, y1=.9),
                           quality=1, face_id='face-1')

    class Engine:
        def infer(self, crop):
            return np.full(crop.shape[:2], bad_value, np.float32)

    class Pool:
        lock = SimpleNamespace(models=[SimpleNamespace(id='glare_mask',
            approved_for_install=True, backends={'cpu': SimpleNamespace(status='passed')})])

        @contextmanager
        def acquire(self, model_id, backend):
            assert model_id == 'glare_mask' and backend == 'cpu'
            yield Engine()

    with pytest.raises(ValueError, match='CAPTURE_GLARE_OUTPUT_INVALID'):
        module.infer_face_evidence(Context(), object(), SimpleNamespace(faces=[face]),
            SimpleNamespace(min_face_edge_px=8), Pool(), 'cpu',
            SimpleNamespace(checks=['glare']))
