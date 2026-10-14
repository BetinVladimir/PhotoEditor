from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import MaskRef
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.semantic_adoption import verify_semantic_mask_pixels
from test_v2_adoption import Reader
from test_image_denoise import Artifacts


@pytest.mark.parametrize('variant',['valid','empty','range','nan','shape','hash','budget','cancel'])
def test_semantic_mask_binary_evidence_and_cleanup(tmp_path,variant):
    a=Artifacts(tmp_path);pixels=np.zeros((20,24),np.float32);pixels[3:8,4:9]=.001
    if variant=='empty':pixels[:]=0
    if variant=='range':pixels[0,0]=1.01
    if variant=='nan':pixels[0,0]=np.nan
    if variant=='shape':pixels=pixels[:10]
    stream=BytesIO();np.save(stream,pixels,allow_pickle=False);body=stream.getvalue()
    ref=ArtifactRef(uuid4(),sha256(body).hexdigest() if variant!='hash' else 'f'*64)
    reader=Reader();reader.add(ref,role='mask_pixels',media='application/x-npy')
    from dataclasses import replace
    reader.rows[ref]=replace(reader.rows[ref],bytes=len(body))
    closed=[]
    def blocks(value,max_bytes):
        try:
            for start in range(0,len(body),37):yield body[start:start+37]
        finally:closed.append(True)
    reader.iter_bytes=blocks
    mask=MaskRef(artifact={'artifact_id':ref.artifact_id,'sha256':ref.sha256},canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,selector='manual',confidence=.8,model_lock_id='a'*64,protected=False)
    scratch=tmp_path/'owned';scratch.mkdir();calls=0
    def active():
        nonlocal calls
        calls+=1
        if variant=='cancel' and calls==8:raise InterruptedError('cancelled')
    def run():return verify_semantic_mask_pixels(reader,mask,scratch_root=scratch,
        max_bytes=len(body)-1 if variant=='budget' else len(body),check_active=active)
    if variant=='valid':assert run()==25
    elif variant=='cancel':
        with pytest.raises(InterruptedError):run()
    else:
        with pytest.raises(AdoptionInvalid):run()
    assert not list(scratch.iterdir())
    if variant!='budget':assert closed==[True]
