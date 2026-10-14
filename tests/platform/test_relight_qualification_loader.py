from copy import deepcopy
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from test_relight_qualification import evidence
from photo_worker.contracts.base import canonical_bytes,canonical_hash,ArtifactRef
from photo_worker.v2.relight_qualification_loader import load_relight_qualification

@pytest.mark.parametrize('mode',['success','short','extra','corrupt','version','bucket','size','stale','pin','runtime','params'])
def test_original_versions_and_live_catalog_revalidation(mode):
    params,pin,report,calibration,policy=evidence()
    bodies={'report':canonical_bytes(report),'calibration':canonical_bytes(calibration)}
    record={'pin':deepcopy(pin),'policy':deepcopy(policy)}
    for key in bodies:
        record[key]=dict(**pin[key],bucket='qualified',object_key=key,object_version='original-'+key,
                         bytes=len(bodies[key]),media_type='application/json')
    if mode=='version':record['report']['object_version']='null'
    if mode=='bucket':record['report']['bucket']='foreign'
    if mode=='size':record['report']['bytes']=True
    raw=canonical_bytes(params)
    lease=SimpleNamespace(command=SimpleNamespace(operation='image.relight',operation_version='1',
        params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params))),
        execution_profile={'relight_qualification':deepcopy(pin),'relight_runtime_sha':'b'*64})
    if mode=='params':raw=b' '+raw
    if mode=='runtime':lease.execution_profile['relight_runtime_sha']='0'*64
    if mode=='pin':record['pin']['qualification_id']=str(uuid4())
    calls=[];streams=[];reads=[]
    def catalog(*args):
        calls.append(args)
        result=deepcopy(record)
        if mode=='stale' and len(calls)>1:result['report']['object_version']='replaced'
        return result
    class Stream(BytesIO):
        released=False
        def release_conn(self):self.released=True
    def get(bucket,key,version_id):
        reads.append((bucket,key,version_id))
        assert version_id=='original-'+key
        body=bodies[key]
        if key=='report':
            if mode=='short':body=body[:-1]
            if mode=='extra':body+=b'x'
            if mode=='corrupt':body=b'x'+body[1:]
        stream=Stream(body);streams.append(stream);return stream
    database=SimpleNamespace(relight_qualification_evidence=catalog)
    objects=SimpleNamespace(get_object=get)
    original=deepcopy(lease)
    if mode=='success':
        rraw,craw,actual,current=load_relight_qualification(database,lease,raw,objects,'qualified',
            expected_runtime_sha='b'*64,check_active=lambda:None)
        assert (rraw,craw,actual)==(bodies['report'],bodies['calibration'],policy)
        assert current()==pin and len(calls)==3
        assert reads==[('qualified','report','original-report'),('qualified','calibration','original-calibration')]
        record['report']['object_version']='changed'
        with pytest.raises(ValueError):current()
    else:
        with pytest.raises(ValueError):load_relight_qualification(database,lease,raw,objects,'qualified',
            expected_runtime_sha='b'*64,check_active=lambda:None)
    assert all(stream.closed and stream.released for stream in streams)
    assert lease==original
