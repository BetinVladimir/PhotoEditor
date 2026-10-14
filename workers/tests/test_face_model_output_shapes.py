"""Reject malformed V27 face-model output before making conversion copies."""

import numpy as np
import pytest

from photo_worker.operations.face.landmarks import _points
from photo_worker.operations.face.parse import decode_logits


def test_face_parse_accepts_only_bounded_native_logits_shapes():
    logits=np.zeros((1,19,2,2),dtype=np.float32)
    labels,confidence=decode_logits(logits,2)
    assert labels.shape==(2,2) and confidence.shape==(2,2)
    for raw in (logits.tolist(),np.zeros((1,19,3,3),dtype=np.float32),
                np.zeros((19,2,3),dtype=np.float32)):
        with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
            decode_logits(raw,2)
    logits[0,0,0,0]=np.nan
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        decode_logits(logits,2)


def test_face_landmarks_require_exact_native_68_by_3_tensor():
    raw=np.zeros((68,3),dtype=np.float32)
    raw[:,2]=1
    assert len(_points(raw,256,256,(0,0,256)))==68
    for wrong in (raw.tolist(),np.zeros((69,3),dtype=np.float32),
                  np.zeros((68,4),dtype=np.float32)):
        with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
            _points(wrong,256,256,(0,0,256))
    raw[0,0]=np.inf
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        _points(raw,256,256,(0,0,256))
