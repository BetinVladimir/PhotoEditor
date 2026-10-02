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
