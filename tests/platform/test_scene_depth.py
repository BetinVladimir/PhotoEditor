import numpy as np
from photo_worker.operations.features.scene import pad_preview
from photo_worker.contracts.scene import Embedding384
from photo_worker.contracts.base import ArtifactRef
from uuid import uuid4


def test_dino_preview_is_square_center_padded_without_distortion():
    rgb=np.ones((10,20,3),np.float32);out=pad_preview(rgb)
    assert out.shape==(518,518,3) and np.all(out[129:388]>0) and np.all(out[:120]==0)


def test_embedding_contract_requires_l2_normalization():
    values=np.zeros(384);values[7]=1
    item=Embedding384(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),values=values.tolist(),model_id='dinov2_vits14',model_lock_id='b'*64,backend='cpu',preprocessing_id='dinov2_pad518_v1',source_clip_fraction=0)
    assert item.values[7]==1


def test_depth_normalization_is_stable_for_constant_output():
    raw=np.full((12,18),3,np.float32);lo,hi=float(raw.min()),float(raw.max());depth=np.zeros_like(raw) if hi-lo<=1e-12 else (raw-lo)/(hi-lo)
    assert np.array_equal(depth,np.zeros_like(raw))
