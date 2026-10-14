"""LR-ASPP output shape is checked before float32 conversion."""

import numpy as np
import pytest

from photo_worker.operations.mask.person import _probability


def test_person_logits_accept_exact_native_shapes():
    raw=np.zeros((1,21,2,3),dtype=np.float32)
    mask,confidence=_probability(raw,2,3)
    assert mask.shape==(2,3) and np.isfinite(mask).all()
    assert 0<confidence<1
    flat,flat_confidence=_probability(raw[0],2,3)
    assert np.array_equal(mask,flat) and confidence==flat_confidence


def test_person_logits_reject_malformed_shape_or_values():
    raw=np.zeros((1,21,2,3),dtype=np.float32)
    for changed in (raw.tolist(),np.zeros((1,21,3,3),dtype=np.float32),
                    np.zeros((1,20,2,3),dtype=np.float32)):
        with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
            _probability(changed,2,3)
    raw[0,15,0,0]=np.nan
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        _probability(raw,2,3)
