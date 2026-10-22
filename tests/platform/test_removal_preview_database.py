from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime.database import TaskDatabase


def lease():
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),
        attempt=3,params=SimpleNamespace(artifact_id=uuid4(),sha256='a'*64))
    return SimpleNamespace(command=command,fence=7,worker_id=uuid4(),input_digest='b'*64)


@pytest.mark.parametrize('method,function,result,expected', [
    ('admit_semantic_removal_preview','admit_semantic_removal_preview',True,True),
    ('admit_semantic_removal_preview','admit_semantic_removal_preview',1,False),
    ('removal_preview_report_evidence','v27_removal_preview_report_evidence',{'pin':{}},{'pin':{}}),
])
def test_removal_query_binds_exact_lease_and_body(method,function,result,expected):
    db=TaskDatabase('unused'); owner=lease();calls=[]
    db._query=lambda sql,args:(calls.append((sql,args)) or (result,))
    assert getattr(db,method)(owner,b'{"schema_version":1}')==expected
    sql,args=calls[0]
    assert 'photo.'+function+'(' in sql
    assert args==(owner.command.tenant_id,owner.command.run_id,owner.command.task_id,
        3,7,owner.worker_id,owner.command.params.artifact_id,'a'*64,'{"schema_version":1}','b'*64)


def test_protection_query_binds_root_and_binary_identity():
    db=TaskDatabase('unused');owner=lease();calls=[]
    root=SimpleNamespace(artifact_id=uuid4(),sha256='c'*64)
    binary=SimpleNamespace(artifact_id=uuid4(),sha256='d'*64)
    db._query=lambda sql,args:(calls.append((sql,args)) or (True,))
    assert db.removal_preview_protection_origin(owner,b'{}',root,binary)
    assert 'photo.semantic_removal_preview_protection_origin(' in calls[0][0]
    assert calls[0][1][-4:]==(root.artifact_id,'c'*64,binary.artifact_id,'d'*64)
