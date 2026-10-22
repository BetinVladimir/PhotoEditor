from hashlib import sha256
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_removal_loader import load_removal_preview_arrays
from test_v27_preview_loader import setup


@pytest.mark.parametrize('failure',[None,'admission','origin','color','budget'])
def test_removal_loader_owns_verified_arrays_and_denies_before_array_io(tmp_path,failure):
    params,artifacts,events=setup(tmp_path)
    intent_id=params['intent']['artifact_id']
    intent=parse_json(artifacts.contract_bytes(SimpleNamespace(artifact_id=intent_id),None))
    intent['kind']='remove_object';intent['ev']=None;raw=canonical_bytes(intent)
    params['intent']['sha256']=sha256(raw).hexdigest()
    params.update(kind='remove_object',model_id='big_lama',context_pixels=128,feather_pixels=8)
    class Documents:
        input=artifacts.input
        def contract_bytes(self,ref,model):
            return raw if str(ref.artifact_id)==intent_id else artifacts.contract_bytes(ref,model)
    options=dict(verify_admission=lambda value:failure!='admission',verify_origin=lambda *args:failure!='origin',
        verify_color=lambda value:failure!='color',max_bytes=64*1048576 if failure=='budget' else 128*1048576,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):load_removal_preview_arrays(Documents(),params,**options)
        assert events==[]
    else:
        result=load_removal_preview_arrays(Documents(),params,**options)
        np.testing.assert_array_equal(result.original,np.full((8,8,3),.5,np.float32))
        assert all(value.flags.owndata for value in (result.original,result.mask,result.protected))
        assert events==['open','open','open','close','close','close']
