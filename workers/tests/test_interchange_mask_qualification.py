from io import BytesIO
from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import Canvas
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.adoption import ArtifactEvidence,ArtifactRef as EvidenceRef
from photo_worker.v2.interchange_parse import ParsedInterchange
from photo_worker.v2.interchange_mask_qualification import ImportedMaskQualifier
from photo_worker.v2.interchange_remap import UnsupportedDependency
from test_interchange_remap import fixture


@pytest.mark.parametrize('mode',['valid','shape','dtype','nan','range'])
def test_mask_qualification_uses_exact_canonical_shape_dtype_and_range(tmp_path,mode):
    parsed,_,_,_,_=fixture()
    array=np.ones((3,4),dtype=np.float32)*np.float32(.5)
    if mode=='shape':array=np.ones((4,3),dtype=np.float32)
    if mode=='dtype':array=array.astype(np.float64)
    if mode=='nan':array[0,0]=np.nan
    if mode=='range':array[0,0]=1.1
    output=BytesIO();np.save(output,array,allow_pickle=False);raw=output.getvalue();digest=sha256(raw).hexdigest()
    canvas=Canvas(width=4,height=3,orientation=1,space_id='canonical',source_sha256='a'*64);canvas_hash=canonical_hash(canvas)
    ref=ArtifactRef(artifact_id=uuid4(),sha256=digest)
    value=parsed.model_dump(mode='json')
    value['manifest']['inventory'].append({'artifact_id':str(uuid4()),'path':'mask.npy','sha256':digest,'contract':'MaskArray'})
    value['files'].append({'path':'mask.npy','content':ref.model_dump(mode='json'),'contract':'MaskArray'})
    value['manifest']['masks'].append({'path':'mask.npy','sha256':digest,'canvas_hash':canvas_hash})
    parsed=ParsedInterchange.model_validate_json(canonical_bytes(value))
    class Reader:
        calls=0
        def evidence(self,r):return ArtifactEvidence(r,'application/octet-stream',len(raw),None,'input')
        def iter_bytes(self,r,maximum):
            self.calls+=1
            yield raw
    reader=Reader()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        qualifier=ImportedMaskQualifier(parsed,{canvas_hash:canvas},reader,scratch)
        if mode=='valid':
            receipt=qualifier('MaskArray',ref)
            assert receipt.shape==(3,4) and receipt.canvas_hash==canvas_hash
            assert qualifier('MaskArray',ref)==receipt and reader.calls==1
        else:
            with pytest.raises((UnsupportedDependency,ValueError)):
                qualifier('MaskArray',ref)
