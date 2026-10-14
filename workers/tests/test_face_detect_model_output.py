"""SFD responses are validated before confidence filtering or NMS."""

import numpy as np
import pytest

from photo_worker.operations.face.detect import _validated_candidates


def test_face_candidates_validate_every_row_including_low_scores():
    rows=np.array([[1,2,6,7,0.9],[3,4,8,9,0.01]],dtype=np.float32)
    assert _validated_candidates(rows,10,10) is rows
    for column,value in ((0,-1),(2,11),(2,3),(4,np.nan),(4,1.1)):
        changed=rows.copy()
        changed[1,column]=value
        with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
            _validated_candidates(changed,10,10)


def test_face_candidates_reject_oversized_or_nonarray_native_output():
    huge=np.zeros((100001,5),dtype=np.float32)
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        _validated_candidates(huge,10,10)
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        _validated_candidates([[1,2,6,7,0.9]],10,10)
