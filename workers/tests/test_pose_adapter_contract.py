"""Torchvision pose output matches the worker's canonical Pose17 ABI."""

import numpy as np
import pytest

from photo_worker.models.adapters import _pose_detection_fields
from photo_worker.operations.pose.detect import _detections, _required_pose_ram


def test_torchvision_score_field_reaches_pose_decoder():
    values={'boxes':np.array([[1,2,11,22]],np.float32),
            'scores':np.array([.9],np.float32),
            'keypoints':np.zeros((1,17,3),np.float32),
            'keypoints_scores':np.full((1,17),.8,np.float32)}
    canonical=_pose_detection_fields(values)
    assert 'keypoint_scores' in canonical and 'keypoints_scores' not in canonical
    accepted,count,truncated=_detections(canonical,100,100,.5,10,'a'*64,'b'*64)
    assert count==1 and len(accepted)==1 and not truncated
    assert len(accepted[0][1])==17


def test_pose_adapter_refuses_missing_native_score_field():
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        _pose_detection_fields({'boxes':None,'scores':None,'keypoints':None,'keypoint_scores':None})


def test_large_pose_frame_exceeds_small_ram_profile():
    assert _required_pose_ram(10_000,10_000,False)>14*1024**3
    assert _required_pose_ram(1024,1024,True)<1024**3
