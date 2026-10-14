from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.negative_adoption_evidence import load_negative_adoption_evidence,negative_adoption_checkpoint
from test_negative_qualification import fixture


@pytest.mark.parametrize('mode',['ok','stale','params','profile','version','changed'])
def test_negative_completed_snapshot_transport_and_refusals(mode):
    p,c,r,pin,policy=fixture();raw=canonical_bytes(p);tenant=uuid4();task=uuid4()
    ctx=SimpleNamespace(attempt=1,fence=2,result_id=uuid4())
    objects=[]
    for contract,ref,body in [('NegativeCalibration',pin['calibration'],c),('QualificationReport',pin['report'],r)]:
        objects.append(dict(contract=contract,**ref,bucket='bucket',object_key=contract,object_version='v1',bytes=len(body)))
    value=dict(schema_version=1,run_id=str(uuid4()),task_id=str(task),attempt=1,fence=2,result_id=str(ctx.result_id),
        params_ref=dict(artifact_id=str(uuid4()),sha256=sha256(raw).hexdigest()),params=p,
        profile=dict(backend='cpu',renderer_version='v2.2',negative_runtime_sha=pin['runtime_lock_sha'],negative_qualification=pin),qualification_objects=objects)
    if mode=='params':value['params_ref']['sha256']='0'*64
    if mode=='profile':value['profile']['backend']='cuda'
    if mode=='version':objects[0]['object_version']='null'
    calls=[]
    class Connection:
        autocommit=True
        def execute(self,query,args):
            assert 'v29_negative_adoption_evidence' in query
            assert args==(tenant,task,1,2,ctx.result_id,raw.decode())
            calls.append(query);return self
        def fetchone(self):return (None if mode=='stale' else deepcopy(value),)
    connection=Connection()
    def load():return load_negative_adoption_evidence(connection,tenant,task,ctx,raw,check_active=lambda:None)
    if mode in ('ok','changed'):
        evidence=load();checkpoint=negative_adoption_checkpoint(connection,tenant,task,ctx,raw,evidence,check_active=lambda:None)
        if mode=='changed':
            value['qualification_objects'][0]['object_version']='v2'
            with pytest.raises(ValueError,match='CHANGED'):checkpoint()
        else:assert checkpoint()==evidence
    else:
        with pytest.raises(ValueError):load()
    assert calls
