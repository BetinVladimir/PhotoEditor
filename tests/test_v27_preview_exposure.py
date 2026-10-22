from hashlib import sha256
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json
from photo_worker.operations.semantic.preview_exposure import PreviewExposureArrays,execute_exposure_preview
from test_v27_exposure_preview_binding import fixture
from test_v27_exposure_source import source_fixture


def inputs():
    params,_,intent=fixture();edit,image,source=source_fixture();params['source']=edit['source']
    candidate=dict(schema_version=1,target=params['source'],mask=edit['mask'],transform=edit['transform'],confidence=.9,state='proposed',reason=None)
    candidate=canonical_bytes(candidate);params['candidate']['sha256']=sha256(candidate).hexdigest()
    return params,candidate,intent,source


@pytest.mark.parametrize('failure',[None,'admission','icc','overlap','budget','loader'])
def test_preview_computes_without_apply_and_preserves_protected_pixels(failure):
    args=inputs();before=np.full((1,2,3),.5,np.float32)
    arrays=PreviewExposureArrays(before,np.array([[0.,.5]],np.float32),np.array([[1.,0.]],np.float32),40,0)
    if failure=='overlap':arrays.protected[0,1]=1
    loader_calls=[]
    def load(*args,**kwargs):
        loader_calls.append(True)
        return None if failure=='loader' else arrays
    options=dict(verify_admission=lambda params:failure!='admission',load_verified_arrays=load,
        verify_color=lambda color:failure!='icc',max_bytes=1 if failure=='budget' else 128*1024**2,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):execute_exposure_preview(*args,**options)
        if failure in ('admission','budget'):assert not loader_calls
    else:
        result=execute_exposure_preview(*args,**options)
        assert np.array_equal(result[0,0].view(np.uint32),before[0,0].view(np.uint32))
        assert np.all(result[0,1]>before[0,1])
        assert np.all(before==.5)
