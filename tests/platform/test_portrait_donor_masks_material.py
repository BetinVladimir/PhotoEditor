import hashlib
import io
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence
from photo_worker.v2.portrait_donor_masks import read_donor_masks
from test_portrait_donor_images import manifest_arguments


def case():
    params,raw,*_=manifest_arguments();source=WorkingImage.model_validate_json(raw);bodies={};docs=[]
    for protected,ref in ((False,params.eyes_mask),(True,params.protected)):
        stream=io.BytesIO();np.save(stream,np.full((100,100),0 if protected else 1,np.float32))
        body=stream.getvalue();pixels=ContractRef(artifact_id=uuid4(),sha256=hashlib.sha256(body).hexdigest())
        bodies[ArtifactRef(pixels.artifact_id,pixels.sha256)]=body
        mask=MaskRef(artifact=pixels,canvas=source.canvas,transform_chain=source.transform_chain,
            selector='manual',confidence=1.,model_lock_id=None,object_id=None,protected=protected)
        doc=canonical_bytes(mask);ref.sha256=hashlib.sha256(doc).hexdigest();docs.append(doc)
    class Reader:
        def evidence(self,ref):
            return ArtifactEvidence(ref,'application/x-npy',len(bodies[ref]),None,'input')
        def iter_bytes(self,ref,max_bytes):
            body=bodies[ref]
            for i in range(0,len(body),65536):yield body[i:i+65536]
    return params,raw,docs,Reader()


def test_pinned_masks_load_exact_canvas_readonly_and_cleanup(tmp_path):
    params,raw,docs,reader=case()
    with read_donor_masks(params,raw,*docs,reader,scratch_root=tmp_path,max_bytes=1_000_000,
                          check_active=lambda:None) as (target,guard,pixels,protected):
        np.testing.assert_array_equal(pixels,1);np.testing.assert_array_equal(protected,0)
        assert not pixels.flags.writeable and not protected.flags.writeable
        assert not target.protected and guard.protected
    assert not list(tmp_path.iterdir())


def test_changed_mask_document_denied_before_reader(tmp_path):
    params,raw,docs,_=case()
    with pytest.raises(ValueError,match='DONOR_MASK_SHA'):
        with read_donor_masks(params,raw,docs[0]+b' ',docs[1],None,scratch_root=tmp_path,
                max_bytes=1_000_000,check_active=lambda:None):
            pytest.fail('Forged mask loaded')
