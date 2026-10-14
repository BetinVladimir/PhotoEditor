import hashlib
import json
import numpy as np
import pytest
from test_portrait_donor_evidence import evidence
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.v2.portrait_open_eyes_prepare import prepare_open_eyes_patch


def arguments():
    p,proof,docs=evidence()
    points=np.random.default_rng(19).uniform(.2,.8,(68,2))
    for doc,key in zip(docs,['source_faces','donor_faces']):
        doc['faces'][0]['landmarks']=[{'x':float(x),'y':float(y)} for x,y in points]
        p[key]['sha256']=hashlib.sha256(canonical_bytes(doc)).hexdigest()
    params=PortraitOpenEyesParams.model_validate_json(json.dumps(p))
    source=np.ones((100,100,3),np.float32);donor=np.full_like(source,2)
    mask=np.zeros((100,100),np.float32);mask[40:60,40:60]=1
    protected=np.zeros_like(mask)
    return params,proof,*[canonical_bytes(d) for d in docs],source,donor,mask,protected,(100,100),(100,100),(0,0),40


def test_evidence_to_patch_requires_repeated_live_authority():
    args=arguments();calls=[]
    def authority(params,proof):
        calls.append(1)
        # External authority receives a copy, not the pipeline's pinned intent.
        params.strength=0
        return True
    result=prepare_open_eyes_patch(*args,lambda:None,authority)
    assert len(calls)==3 and result.requires_review
    assert result.pixels[50,50,0]>1
    np.testing.assert_array_equal(result.pixels[0],1)


@pytest.mark.parametrize('deny_at',[1,2,3])
def test_revoked_authority_at_each_phase_returns_no_result(deny_at):
    calls=0
    def authority(params,proof):
        nonlocal calls
        calls+=1
        return calls!=deny_at
    with pytest.raises(ValueError,match='DONOR_AUTHORITY'):
        prepare_open_eyes_patch(*arguments(),lambda:None,authority)
