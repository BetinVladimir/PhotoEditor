from copy import deepcopy
from io import BytesIO
from uuid import uuid4
import pytest
from photo_worker.v2 import negative_qualification_objects as module
from test_negative_qualification import fixture


@pytest.mark.parametrize('mode',['ok','changed','oversize','corrupt','bucket'])
def test_negative_original_objects_current_metadata_and_cleanup(monkeypatch,mode):
    p,c,r,pin,policy=fixture();calls=[];closed=[]
    locs=[dict(contract=name,artifact_id=pin[key]['artifact_id'],sha256=pin[key]['sha256'],bucket='bucket',
        object_key=key,object_version='v1',bytes=len(body)) for name,key,body in
        [('NegativeCalibration','calibration',c),('QualificationReport','report',r)]]
    def catalog(*args,**kwargs):
        value=deepcopy(locs)
        if mode=='changed' and calls:value[0]['object_version']='v2'
        return value
    monkeypatch.setattr(module,'resolve_negative_qualification_objects',catalog)
    class Response(BytesIO):
        def close(self):closed.append('close');super().close()
        def release_conn(self):closed.append('release')
    class Objects:
        def get_object(self,bucket,key,version_id):
            assert bucket=='bucket' and version_id=='v1';calls.append(key)
            body=c if key=='calibration' else r
            if mode=='oversize':body+=b' '
            if mode=='corrupt':body=b'!'+body[1:]
            return Response(body)
    def run():return module.load_negative_qualification_objects(None,Objects(),*[uuid4() for _ in range(4)],p,pin,policy,
        bucket='other' if mode=='bucket' else 'bucket',input_icc_sha='c'*64,runtime_sha='b'*64,check_active=lambda:None)
    if mode=='ok':assert run().calibration_raw==c and calls==['calibration','report']
    else:
        with pytest.raises(ValueError):run()
    assert closed==['close','release']*len(calls)
    if mode in ('changed','corrupt','oversize'):assert calls==['calibration']
    if mode=='bucket':assert calls==[]
