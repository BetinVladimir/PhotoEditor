import numpy as np
import pytest
from photo_worker.algorithms.defect_labels import validate_labels
from test_defect_review import setup


def label_map(proposal):
    out=np.zeros((proposal.canvas.height,proposal.canvas.width),np.uint16)
    for c in proposal.candidates:
        r=c.bbox;out[r.y:r.y+r.height,r.x:r.x+r.width]=c.label
    return out


def test_exact_labels_counts_and_strided_input(tmp_path):
    a,ref,proposal,review=setup(tmp_path);labels=label_map(proposal)
    result=validate_labels(labels,proposal)
    assert [(r.label,r.pixels) for r in result]==[(1,9),(2,9)]
    backing=np.zeros((labels.shape[0],labels.shape[1]*2),np.uint16);backing[:,::2]=labels
    assert validate_labels(backing[:,::2],proposal)==result


def test_unknown_missing_and_bbox_mismatch(tmp_path):
    a,ref,proposal,review=setup(tmp_path);labels=label_map(proposal)
    extra=labels.copy();extra[0,0]=99
    with pytest.raises(ValueError,match='UNKNOWN'):validate_labels(extra,proposal)
    missing=labels.copy();missing[missing==1]=0
    with pytest.raises(ValueError,match='MISSING'):validate_labels(missing,proposal)
    labels[0,0]=1
    with pytest.raises(ValueError,match='BBOX'):validate_labels(labels,proposal)


def test_cancel_and_invalid_layout(tmp_path):
    a,ref,proposal,review=setup(tmp_path);labels=label_map(proposal)
    with pytest.raises(ValueError,match='LAYOUT'):validate_labels(labels.astype(np.int32),proposal)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):validate_labels(labels,proposal,check_active=cancel)
