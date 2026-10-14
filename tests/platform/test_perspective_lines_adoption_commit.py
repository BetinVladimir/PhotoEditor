from dataclasses import replace
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
from photo_worker.v2.perspective_lines_adoption_prepare import PreparedPerspectiveLinesAdoption
from photo_worker.v2.perspective_lines_adoption_commit import perspective_lines_commit_statement


def fixture():
    tenant,task,result=uuid4(),uuid4(),uuid4()
    root=b'{"status":"unsupported"}'
    params=b'{"schema_version":1}'
    context=SimpleNamespace(result_id=result,attempt=1,fence=7,root=ArtifactRef(uuid4(),sha256(root).hexdigest()))
    evidence=canonical_bytes(dict(task_id=str(task),result_id=str(result),attempt=1,fence=7,
        root=dict(artifact_id=str(context.root.artifact_id),sha256=context.root.sha256),
        params_ref=dict(artifact_id=str(uuid4()),sha256=sha256(params).hexdigest())))
    return tenant,task,context,PreparedPerspectiveLinesAdoption(evidence,params,root,None,None)


def test_commit_preserves_original_nested_json_and_fence():
    tenant,task,context,prepared=fixture()
    inventory=[{'artifact_id':str(context.root.artifact_id)}]
    statement,args=perspective_lines_commit_statement(tenant,task,context,inventory,'a'*64,prepared)
    assert statement.count('%s')==10 and 'adopt_v29_perspective_lines' in statement
    assert args[:7]==(tenant,task,1,7,context.result_id,inventory,'a'*64)
    assert args[7:]==(prepared.evidence_jcs.decode(),prepared.params_bytes.decode(),prepared.result_bytes.decode())


def test_commit_refuses_changed_result_params_and_context():
    tenant,task,context,prepared=fixture()
    for changed in [replace(prepared,result_bytes=prepared.result_bytes+b' '),
                    replace(prepared,params_bytes=prepared.params_bytes+b' ')]:
        with pytest.raises(AdoptionInvalid):perspective_lines_commit_statement(tenant,task,context,[],'a'*64,changed)
    context.fence+=1
    with pytest.raises(AdoptionInvalid):perspective_lines_commit_statement(tenant,task,context,[],'a'*64,prepared)
