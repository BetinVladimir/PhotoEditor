import numpy as np
import pytest

from photo_worker.operations.mask.face_skin import skin_probability


def test_skin_probability_selects_only_face19_skin_class():
    labels = np.array([[0, 1, 2], [10, 1, 18]], dtype=np.uint16)
    result = skin_probability(labels)
    np.testing.assert_array_equal(result, [[0, 1, 0], [0, 1, 0]])
    assert result.dtype == np.float32


def test_skin_probability_rejects_invalid_label_map():
    with pytest.raises(ValueError):
        skin_probability(np.array([[19]], dtype=np.uint16))
    with pytest.raises(ValueError):
        skin_probability(np.array([[1]], dtype=np.int32))


def test_skin_probability_preserves_noncontiguous_view():
    labels = np.array([[1, 2, 1, 18], [0, 1, 10, 1]], dtype=np.uint16)[:, ::-2]
    assert not labels.flags.c_contiguous
    np.testing.assert_array_equal(skin_probability(labels), (labels == 1).astype(np.float32))


def test_skin_probability_checks_cancellation_between_blocks():
    labels = np.ones((1, 40000), dtype=np.uint16)
    checks = 0

    def check_active():
        nonlocal checks
        checks += 1
        if checks == 3:
            raise RuntimeError('cancelled')

    with pytest.raises(RuntimeError, match='cancelled'):
        skin_probability(labels, check_active)
    assert checks == 3


def test_skin_probability_rejects_invalid_late_block_and_empty_canvas():
    labels = np.ones((1, 40000), dtype=np.uint16)
    labels[0, -1] = 19
    with pytest.raises(ValueError):
        skin_probability(labels)
    with pytest.raises(ValueError):
        skin_probability(np.zeros((0, 1), dtype=np.uint16))
