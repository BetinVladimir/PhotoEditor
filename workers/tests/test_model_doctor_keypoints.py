import numpy as np
import pytest

from photo_worker.models.doctor import smoke
from photo_worker.models.install import ModelError


MODEL_ID = 'keypointrcnn_resnet50_fpn'


class PoseModel:
    def __init__(self, values):
        self.values = values

    def infer(self, image):
        assert image.shape == (640, 640, 3)
        return self.values


def output(count):
    return {
        'boxes': np.zeros((count, 4), dtype=np.float32),
        'scores': np.zeros((count,), dtype=np.float32),
        'keypoints': np.zeros((count, 17, 3), dtype=np.float32),
        'keypoints_scores': np.zeros((count, 17), dtype=np.float32),
    }


@pytest.mark.parametrize('count', [0, 2])
def test_doctor_accepts_consistent_empty_and_detected_pose_shapes(count):
    result = smoke(MODEL_ID, PoseModel(output(count)))
    assert result['shape'] == [count * (4 + 1 + 17 * 3 + 17)]


@pytest.mark.parametrize('field,replacement', [
    ('boxes', np.zeros((2, 5), dtype=np.float32)),
    ('scores', np.zeros((1,), dtype=np.float32)),
    ('keypoints', np.zeros((2, 16, 3), dtype=np.float32)),
    ('keypoints_scores', np.zeros((2, 16), dtype=np.float32)),
    ('scores', np.full((2,), 1.1, dtype=np.float32)),
    ('keypoints_scores', np.full((2, 17), np.nan, dtype=np.float32)),
])
def test_doctor_refuses_malformed_pose_output(field, replacement):
    values = output(2)
    values[field] = replacement
    with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
        smoke(MODEL_ID, PoseModel(values))
