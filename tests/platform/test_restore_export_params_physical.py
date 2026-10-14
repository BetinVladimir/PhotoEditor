from contextlib import contextmanager
from io import BytesIO
from hashlib import sha256
from dataclasses import replace
from uuid import uuid4
import pytest
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_export_plan as m


@pytest.mark.parametrize('mode',['valid','corrupt','truncated','changed','oversize'])
def test_export_params_exact_version_bytes_and_catalogue_recheck(monkeypatch,mode):
    raw=b'{"format":"jpeg"}';digest=sha256(raw).hexdigest()
    obj=RestoreObject('photos','tenants/fixture/params','v1',digest,1048577 if mode=='oversize' else len(raw))
    calls=[]
    def observe(*a,**k):
        calls.append(True)
        return digest,replace(obj,version='v2') if mode=='changed' and len(calls)>1 else obj,raw
    monkeypatch.setattr(m,'verify_fixture_export_plan',observe)
    class Store:
        def __init__(self,*a,**k):pass
        @contextmanager
        def open_version(self,value):
            assert value==obj
            if mode=='oversize':raise AssertionError('unbounded IO')
            yield BytesIO(b'bad' if mode=='corrupt' else raw[:-1] if mode=='truncated' else raw)
    monkeypatch.setattr(m,'MinioRestoreObjects',Store)
    def invoke():return m.verify_fixture_export_params(None,None,['photos'],uuid4(),None,None,
        expected_database='restore',check_active=lambda:None)
    if mode=='valid':assert invoke()==digest and len(calls)==2
    else:
        with pytest.raises(ValueError):invoke()
