from io import BytesIO
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_render_array import verify_restore_render_array


@pytest.mark.parametrize('mode',['pixels','alpha','mask','nan','alpha_range','nonbinary','wrong_shape','wrong_sha','wrong_version','fixture_false','activity_false'])
def test_exact_version_arrays_use_real_streaming_adoption_validator(mode):
    role='pixels' if mode in ('pixels','nan') else 'alpha' if mode in ('alpha','alpha_range') else 'mask_pixels'
    shape=(2,2,3) if role=='pixels' else (2,2);array=np.ones(shape,dtype='<f4')
    if mode=='nan':array.flat[0]=np.nan
    if mode=='alpha_range':array.flat[0]=1.1
    if mode=='nonbinary':array.flat[0]=.5
    stream=BytesIO();np.save(stream,array,allow_pickle=False);raw=stream.getvalue();pin=sha256(raw).hexdigest()
    if mode=='wrong_sha':pin='0'*64
    obj=RestoreObject('photos','tenants/'+str(uuid4())+'/array','v1',pin,len(raw))
    ref=ArtifactRef(artifact_id=uuid4(),sha256=pin);closed=[];requests=[]
    class Client:
        def get_object(self,bucket,key,version_id):
            requests.append(True)
            assert version_id=='v1';parts=[raw[:37],raw[37:],b'']
            return NS(status=200,headers={'x-amz-version-id':'v2' if mode=='wrong_version' else 'v1','content-length':str(len(raw))},
                read=lambda size,decode_content=False:parts.pop(0),close=lambda:closed.append(True),release_conn=lambda:None)
    def run():return verify_restore_render_array(Client(),('photos',),ref,obj,(3,2) if mode=='wrong_shape' else shape,role,
        assert_fixture_scope=lambda *args:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode in ('pixels','alpha','mask'):assert run().shape==shape
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):assert requests==[] and closed==[]
    else:assert closed==[True]
