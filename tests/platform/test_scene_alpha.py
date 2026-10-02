import numpy as np
from photo_worker.operations.scene.parse import decode_logits
from photo_worker.operations.alpha.predict import normalize_output


def test_scene_logits_restore_nearest_labels_and_confidence():
    logits=np.zeros((150,8,12),np.float32);logits[7]=4
    labels,confidence=decode_logits(logits,24,16)
    assert labels.shape==(16,24) and np.all(labels==7) and confidence>.2


def test_u2net_output_minmax_and_constant_are_defined():
    raw=np.arange(320*320,dtype=np.float32).reshape(320,320);value=normalize_output(raw)
    assert value.min()==0 and value.max()==1
    assert not np.any(normalize_output(np.ones((320,320),np.float32)))
