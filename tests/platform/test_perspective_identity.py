from io import BytesIO
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_perspective_adapter import source
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2 import perspective_adoption as module
from photo_worker.v2.lens_bypass import _ref


@pytest.mark.parametrize('nonzero,tampered',[(False,False),(True,False),(False,True)])
def test_identity_requires_exact_canonical_zero_mask(monkeypatch,nonzero,tampered):
    image=source();source_ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    values=np.zeros((image.canvas.height,image.canvas.width),np.float32)
    if nonzero:values[0,0]=1
    stream=BytesIO();np.save(stream,values,allow_pickle=False);raw=stream.getvalue()
    pixel_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    mask=SimpleNamespace(artifact=pixel_ref)
    params=SimpleNamespace(strength=0,base=SimpleNamespace(source=source_ref))
    result=SimpleNamespace(changed_mask=source_ref)
    monkeypatch.setattr(module,'verify_perspective_result_metadata',lambda *args,**kwargs:(params,result))
    monkeypatch.setattr(module,'read_document',lambda reader,ref,model,origin:image if model is WorkingImage else mask)
    class Reader:
        def evidence(self,ref):return ArtifactEvidence(ref,'application/x-npy',len(raw),'mask_pixels','output')
        def iter_bytes(self,ref,limit):
            payload=bytearray(raw)
            if tampered:payload[-1]=1
            for offset in range(0,len(payload),7):
                yield bytes(payload[offset:offset+7])
    if nonzero or tampered:
        with pytest.raises(ValueError,match='MASK_NONZERO'):
            module.verify_perspective_identity(Reader(),None,None,check_active=lambda:None)
    else:assert module.verify_perspective_identity(Reader(),None,None,check_active=lambda:None)==_ref(source_ref)
