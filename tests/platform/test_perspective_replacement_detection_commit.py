from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2 import perspective_replacement_detection_commit as module
from photo_worker.v2.adoption import AdoptionInvalid


def test_replacement_commit_binds_descriptor_and_preserves_all_arguments(monkeypatch):
    tenant,task,run=uuid4(),uuid4(),uuid4()
    prepared=SimpleNamespace(params_bytes=b'{}',evidence_jcs=canonical_bytes({'run_id':str(run)}))
    descriptor={'tenant_id':str(tenant),'task_id':str(task),'run_id':str(run),
        'params_jcs':'{}','kind':'v29_perspective_replacement_detection_v1'}
    proof={k:'unused' for k in ('actor_id','auth_session_id','decision_hash','original_jcs','project_id','review_id','snapshot_id')}
    proof.update(descriptor_jcs=canonical_bytes(descriptor).decode(),plan={})
    args=tuple(range(10))
    monkeypatch.setattr(module,'perspective_lines_commit_statement',lambda *a:('unused',args))
    raw=canonical_bytes(proof)
    sql,actual=module.replacement_detection_commit_statement(tenant,task,None,None,None,prepared,raw)
    assert sql.count('%s')==11 and 'adopt_v29_replacement_detector(' in sql
    assert actual==args+(raw.decode(),)
    descriptor['task_id']=str(uuid4())
    proof['descriptor_jcs']=canonical_bytes(descriptor).decode()
    with pytest.raises(AdoptionInvalid,match='COMMIT_PROOF_INVALID'):
        module.replacement_detection_commit_statement(tenant,task,None,None,None,prepared,canonical_bytes(proof))
