import numpy as np
from photo_worker.operations.face.parse import decode_logits,crop_geometry
from photo_worker.contracts.images import NormalizedRect


def test_face_parse_logits_accept_runtime_hwc_and_are_deterministic():
    logits=np.zeros((512,512,19),np.float32);logits[...,3]=5
    labels,confidence=decode_logits(logits)
    assert np.all(labels==3) and confidence.mean()>.8


def test_face_crop_geometry_maps_box_center_to_crop_center():
    box=NormalizedRect(x0=.25,y0=.2,x1=.75,y1=.8);matrix,_=crop_geometry(box,100,80)
    point=matrix@np.array([50,40,1],np.float32)
    assert np.allclose(point,[256,256])
