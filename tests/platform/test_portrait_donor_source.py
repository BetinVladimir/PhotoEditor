from copy import deepcopy
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,ArtifactRef
from photo_worker.runtime.database import TaskDatabase
from photo_worker.v2.portrait_donor_source import donor_source_authority
from test_portrait_open_eyes_prepare import arguments


def test_donor_source_authority_repeats_sql_with_owned_pins():
    args=arguments();params,proof=args[0],args[1];body=canonical_bytes(params)
    lease=SimpleNamespace(command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',params=SimpleNamespace(sha256=sha256(body).hexdigest())))
    ready=[True];calls=[]
    def live(owned,raw,confirmation):
        assert raw==body and confirmation==proof
        owned.command.operation='changed';calls.append(True);return ready[0]
    authority=donor_source_authority(SimpleNamespace(donor_source_ready=live),lease,body,lambda:None)
    lease.command.operation='changed'
    assert authority(params,proof) is True and authority(params,proof) is True
    changed=params.model_copy(deep=True);changed.strength=0
    with pytest.raises(ValueError):authority(changed,proof)
    with pytest.raises(ValueError):authority(params,b' '+proof)
    assert len(calls)==2
    ready[0]=False
    with pytest.raises(ValueError):authority(params,proof)
    assert len(calls)==3


@pytest.mark.parametrize('row,expected',[(None,False),((False,),False),((True,),True),((1,),False)])
def test_donor_source_sql_transport(row,expected):
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=3,params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
    lease=SimpleNamespace(command=command,fence=17,worker_id=uuid4(),input_digest='b'*64)
    db=TaskDatabase('unused');calls=[]
    def query(sql,args):calls.append((sql,args));return row
    db._query=query
    assert db.donor_source_ready(lease,b'{}',b'{}') is expected
    assert calls==[('SELECT photo.v28_worker_donor_source_ready('+','.join(['%s']*11)+')',
        (command.tenant_id,command.run_id,command.task_id,3,17,lease.worker_id,command.params.artifact_id,command.params.sha256,'{}',lease.input_digest,'{}'))]
