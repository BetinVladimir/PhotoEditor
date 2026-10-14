from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_legacy_array as m


@pytest.mark.parametrize('mode',['rgb','uint16_alpha','float_alpha','hdr','nan','bad_alpha','hash','header','activity_false'])
def test_legacy_stream_preserves_hdr_and_uint16_alpha_but_rejects_corruption(monkeypatch,mode):
    alpha=mode in ('uint16_alpha','float_alpha','bad_alpha')
    dtype='uint16' if mode=='uint16_alpha' else 'float32';shape=(2,2) if alpha else (2,2,3)
    values=np.ones(shape,dtype=dtype)
    if mode=='hdr':values.flat[0]=2.;values.flat[1]=-.1
    if mode=='nan':values.flat[0]=np.nan
    if mode=='bad_alpha':values.flat[0]=2.
    stream=BytesIO();np.save(stream,values,allow_pickle=False);raw=stream.getvalue()
    if mode=='header':raw=raw.replace(b'<f4',b'>f4',1)
    digest='f'*64 if mode=='hash' else sha256(raw).hexdigest()
    ref=ArtifactRef(artifact_id=uuid4(),sha256=digest);obj=RestoreObject('photos','tenants/fixture/array','v1',digest,len(raw))
    opens=[]
    class Store:
        def __init__(self,*a,**k):pass
        @contextmanager
        def open_version(self,value):
            opens.append(True);assert value==obj;yield BytesIO(raw)
    monkeypatch.setattr(m,'MinioRestoreObjects',Store)
    def run():return m.verify_legacy_array_bytes(object(),('photos',),ref,obj,shape,dtype,
        'working_alpha' if alpha else 'working_rgb',
        check_active=lambda:False if mode=='activity_false' else None)
    if mode in ('rgb','uint16_alpha','float_alpha','hdr'):assert run()==obj
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode=='activity_false':assert opens==[]
