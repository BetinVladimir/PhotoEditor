from hashlib import sha256
import json
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.admission import SQLRegionAdmission
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.database import TaskDatabase
from test_region_preview_contract import region_fixture


@pytest.mark.parametrize('case',['accepted','denied','digest','tenant','backend'])
def test_region_sql_admission_checks_scope_before_database(case):
    plan,_=region_fixture()
    params=RegionPreviewParams.model_validate_json(json.dumps(plan))
    raw=canonical_bytes(params)
    command=SimpleNamespace(operation='region.preview',operation_version='1',resource_class='gpu',tenant_id=params.tenant_id,params=SimpleNamespace(sha256=sha256(raw).hexdigest()))
    lease=SimpleNamespace(command=command,execution_profile=dict(backend='cuda',renderer_version='v2.2'))
    calls=[]
    class DB:
        def admit_region_preview(self, actual, body):
            calls.append((actual,body))
            return case!='denied'
    if case=='digest':command.params.sha256='0'*64
    if case=='tenant':command.tenant_id=uuid4()
    if case=='backend':lease.execution_profile['backend']='cpu'
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    if case=='accepted':SQLRegionAdmission(DB()).admit(ctx,params)
    else:
        with pytest.raises(OperationFailure):SQLRegionAdmission(DB()).admit(ctx,params)
    assert len(calls)==(1 if case in ('accepted','denied') else 0)
    if calls:assert calls[0]==(lease,raw)


@pytest.mark.parametrize('row,expected',[(None,False),((False,),False),((True,),True),((1,),False)])
def test_region_database_transport_preserves_exact_generation(row,expected):
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=7,
                            params=SimpleNamespace(artifact_id=uuid4(),sha256='a'*64))
    lease=SimpleNamespace(command=command,fence=19,worker_id=uuid4(),input_digest='b'*64)
    db=TaskDatabase('unused')
    calls=[]
    def query(sql,args):
        calls.append((sql,args));return row
    db._query=query
    assert db.admit_region_preview(lease,b'{"test":true}') is expected
    assert len(calls)==1
    assert 'admit_v26_region_preview' in calls[0][0]
    assert calls[0][1]==(command.tenant_id,command.run_id,command.task_id,7,19,lease.worker_id,
                          command.params.artifact_id,'a'*64,'{"test":true}','b'*64)
