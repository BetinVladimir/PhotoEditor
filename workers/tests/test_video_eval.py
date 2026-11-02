import pytest

from photo_worker.video.eval import iou, prf, score_boundaries
from video_vectors import load_vector

V = load_vector('f1')


def test_f1_vector_cases():
    assert len(V) == 10
    for c in V:
        counts = score_boundaries(c['pred'], c['gt'])
        e = c['expect']
        assert counts == {k: e[k] for k in ('tp', 'fp', 'fn')}, c['id']
        got = prf(**counts)
        for k in ('precision', 'recall', 'f1'):
            assert got[k] == pytest.approx(e[k], abs=1e-4), (c['id'], k)


def test_iou_edges():
    assert iou((0, 10), (0, 10)) == 1
    assert iou((0, 10), (10, 20)) == 0
    assert iou((5, 5), (5, 5)) == 0
