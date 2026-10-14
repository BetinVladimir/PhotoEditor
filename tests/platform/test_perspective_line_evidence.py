from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,ArtifactRef
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.operations.optical import perspective_line_evidence as module

def fixture():
    image=WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),alpha=None,dtype='float32',canvas=Canvas(width=100,height=80,orientation=1,space_id='source',source_sha256='b'*64),color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    raw=canonical_bytes(image)
    return raw,sha256(raw).hexdigest(),np.full((80,100,3),2.,dtype=np.float32)

def test_absent_candidates_keep_exact_source_and_hdr(monkeypatch):
    raw,digest,rgb=fixture();before=rgb.copy()
    monkeypatch.setattr(module,'detect_perspective_lines',lambda *args,**kwargs:None)
    evidence=module.prepare_detected_perspective_lines(raw,digest,rgb,None,working_icc_sha='c'*64)
    assert evidence.source_sha256==digest and evidence.lines is None and evidence.lines_sha256 is None
    assert np.array_equal(rgb,before)

def test_source_hash_icc_and_full_array_validation(monkeypatch):
    raw,digest,rgb=fixture();called=[]
    monkeypatch.setattr(module,'detect_perspective_lines',lambda *args,**kwargs:called.append(True))
    with pytest.raises(ValueError):module.prepare_detected_perspective_lines(raw,'d'*64,rgb,None,working_icc_sha='c'*64)
    with pytest.raises(ValueError):module.prepare_detected_perspective_lines(raw,digest,rgb,None,working_icc_sha='d'*64)
    rgb[1,1,0]=np.nan
    with pytest.raises(ValueError):module.prepare_detected_perspective_lines(raw,digest,rgb,None,working_icc_sha='c'*64)
    assert called==[]
