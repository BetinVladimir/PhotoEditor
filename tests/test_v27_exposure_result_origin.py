from types import SimpleNamespace
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,ArtifactRef as ContractRef
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.v2.adoption import ArtifactRef
from photo_worker.v2 import exposure_origin as module
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('accepted',[True,False,1,None])
def test_result_origin_owned_scope_and_source_substitution(monkeypatch,accepted):
    edit,_,_=source_fixture();edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    raw=canonical_bytes(edit);params=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    context=SimpleNamespace(attempt=1,fence=2,result_id=uuid4());tenant,task=uuid4(),uuid4();calls=[]
    monkeypatch.setattr(module,'load_exposure_adoption_params',lambda *a,**k:(params,edit))
    def execute(sql,args):calls.append(args);return SimpleNamespace(fetchone=lambda:(accepted,))
    verify=module.sql_exposure_adoption_origin(SimpleNamespace(execute=execute),tenant,task,
        context,SimpleNamespace(read_json=lambda *a:raw),check_active=lambda:None)
    root=ContractRef(artifact_id=uuid4(),sha256='a'*64);binary=ContractRef(artifact_id=uuid4(),sha256='b'*64)
    context.fence=99
    assert verify(edit.source,root,binary) is (accepted is True)
    assert calls[0]==(tenant,task,1,2,context.result_id,params.artifact_id,params.sha256,
        raw.decode(),root.artifact_id,root.sha256,binary.artifact_id,binary.sha256)
    changed=edit.source.model_copy(update={'canvas_hash':'f'*64})
    assert verify(changed,root,binary) is False and len(calls)==1
