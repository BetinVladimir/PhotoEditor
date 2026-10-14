from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.database import TaskDatabase

@pytest.mark.parametrize('answer',[None,(None,),({'pin':'fixture'},)])
def test_relight_evidence_query_exact_attempt_arguments(answer):
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=3,
        params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
    lease=SimpleNamespace(command=command,fence=7,worker_id=uuid4(),input_digest='b'*64)
    database=object.__new__(TaskDatabase);calls=[]
    def query(sql,args):calls.append((sql,args));return answer
    database._query=query
    assert database.relight_qualification_evidence(lease,b'{}')==(None if answer is None else answer[0])
    assert calls==[('SELECT photo.v28_worker_relight_qualification_evidence('+','.join(['%s']*10)+')',
        (command.tenant_id,command.run_id,command.task_id,3,7,lease.worker_id,
         command.params.artifact_id,'a'*64,'{}','b'*64))]


@pytest.mark.parametrize('root',[None,b'{"root":1}'])
def test_relight_manual_origin_exact_attempt_and_optional_root(root):
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=3,
        params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
    lease=SimpleNamespace(command=command,fence=7,worker_id=uuid4(),input_digest='b'*64)
    database=object.__new__(TaskDatabase);calls=[]
    def query(sql,args):calls.append((sql,args));return ({'root':'fixture'},)
    database._query=query
    assert database.relight_manual_origin(lease,b'{}',b'{"mask":1}',root,True)=={'root':'fixture'}
    assert calls==[('SELECT photo.v28_worker_relight_manual_origin('+','.join(['%s']*13)+')',
        (command.tenant_id,command.run_id,command.task_id,3,7,lease.worker_id,
         command.params.artifact_id,'a'*64,'{}','b'*64,'{"mask":1}',None if root is None else root.decode(),True))]
