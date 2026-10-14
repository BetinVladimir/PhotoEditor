from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.perspective_adoption_evidence import load_perspective_adoption_evidence,perspective_adoption_checkpoint
from test_perspective_evidence import fixture


@pytest.mark.parametrize('mode',['ok','stale','params','profile','version','changed'])
def test_completed_perspective_snapshot_transport_and_refusals(mode):
    raw,lease,record,bodies=fixture();tenant=uuid4();task=uuid4();ctx=SimpleNamespace(attempt=1,fence=2,result_id=uuid4())
    objects=[]
    for label,contract in [('calibration','Calibration'),('report','Report')]:
        item=record[label];objects.append({key:value for key,value in item.items() if key!='media_type'}|{'contract':contract})
    from photo_worker.contracts.base import parse_json
    value=dict(schema_version=1,run_id=str(uuid4()),task_id=str(task),attempt=1,fence=2,result_id=str(ctx.result_id),
        params_ref=dict(artifact_id=str(uuid4()),sha256=sha256(raw).hexdigest()),params=parse_json(raw),
        profile=lease.execution_profile,qualification_objects=objects)
    if mode=='params':value['params_ref']['sha256']='0'*64
    if mode=='profile':value['profile']['backend']='cuda'
    if mode=='version':objects[0]['object_version']='null'
    class Connection:
        autocommit=True
        def execute(self,query,args):
            assert 'v29_perspective_adoption_evidence' in query
            assert args==(tenant,task,1,2,ctx.result_id,raw.decode());return self
        def fetchone(self):return (None if mode=='stale' else deepcopy(value),)
    connection=Connection()
    def load():return load_perspective_adoption_evidence(connection,tenant,task,ctx,raw,check_active=lambda:None)
    if mode in ('ok','changed'):
        evidence=load();checkpoint=perspective_adoption_checkpoint(connection,tenant,task,ctx,raw,evidence,check_active=lambda:None)
        if mode=='changed':
            objects[0]['object_version']='version-2'
            with pytest.raises(ValueError,match='CHANGED'):checkpoint()
        else:assert checkpoint()==evidence
    else:
        with pytest.raises(ValueError):load()
