from copy import deepcopy
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.negative_qualification_loader import load_negative_qualification
from test_negative_qualification import fixture


@pytest.mark.parametrize('mode',['ok','changed','corrupt','oversize','metadata'])
def test_negative_worker_original_evidence_and_current_sql(mode):
    p,c,r,pin,policy=fixture();body=canonical_bytes(p);reads=[];closed=[]
    lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',
        params=SimpleNamespace(sha256=sha256(body).hexdigest())),
        execution_profile={'negative_qualification':deepcopy(pin),'negative_runtime_sha':pin['runtime_lock_sha']})
    record=dict(pin=pin,policy=policy)
    for label,raw in [('report',r),('calibration',c)]:
        record[label]=dict(artifact_id=pin[label]['artifact_id'],sha256=pin[label]['sha256'],
            bucket='bucket',object_key=label,object_version='v1',bytes=len(raw),media_type='application/json')
    if mode=='metadata':record['calibration']['object_version']='null'
    def current(*args):
        value=deepcopy(record)
        if mode=='changed' and reads:value['calibration']['object_version']='v2'
        return value
    class Stream(BytesIO):
        def close(self):closed.append('close');super().close()
        def release_conn(self):closed.append('release')
    class Objects:
        def get_object(self,bucket,key,version_id):
            reads.append(key);raw=c if key=='calibration' else r
            if mode=='corrupt':raw=b'!'+raw[1:]
            if mode=='oversize':raw+=b' '
            return Stream(raw)
    def run():return load_negative_qualification(SimpleNamespace(negative_qualification_evidence=current),lease,
        body,Objects(),'bucket',input_icc_sha='c'*64,check_active=lambda:None)
    if mode=='ok':
        report,calibration,owned_policy,current_pin=run()
        assert report==r and calibration==c and current_pin()==pin
        owned_policy['required_checks'].clear();assert current_pin()==pin
    else:
        with pytest.raises(ValueError):run()
    assert closed==['close','release']*len(reads)
    if mode=='metadata':assert reads==[]
    elif mode!='ok':assert reads==['calibration']
