"""Own retouch loader with real pinned NPY and external authority doubles."""
from hashlib import sha256
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_retouch_loader import load_retouch_preview_arrays
from test_v27_preview_loader import setup


def retouch_documents(tmp_path):
    params,artifacts,events=setup(tmp_path)
    intent_id=params['intent']['artifact_id']
    intent=parse_json(artifacts.contract_bytes(SimpleNamespace(artifact_id=intent_id),None))
    intent.update(kind='retouch_region',ev=None)
    raw=canonical_bytes(intent);params['intent']['sha256']=sha256(raw).hexdigest()
    params['kind']='retouch_region'
    class Documents:
        input=artifacts.input
        def contract_bytes(self,ref,model):
            return raw if str(ref.artifact_id)==intent_id else artifacts.contract_bytes(ref,model)
    return params,Documents(),events


@pytest.mark.parametrize('failure',[None,'admission','origin','color','budget'])
def test_retouch_loader_owns_arrays_and_preserves_own_kind(tmp_path,failure):
    params,artifacts,events=retouch_documents(tmp_path);seen=[]
    def admission(value):seen.append(value.kind);return failure!='admission'
    def run():return load_retouch_preview_arrays(artifacts,params,verify_admission=admission,
        verify_origin=lambda *args:failure!='origin',verify_color=lambda *args:failure!='color',
        max_bytes=64*1048576 if failure=='budget' else 128*1048576,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):run()
        assert events==[]
    else:
        result=run();np.testing.assert_array_equal(result.original,np.full((8,8,3),.5,np.float32))
        assert all(array.flags.owndata for array in (result.original,result.mask,result.protected))
        assert events==['open','open','open','close','close','close']
    assert seen and set(seen)=={'retouch_region'}
