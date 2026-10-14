from types import SimpleNamespace
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.v2.adoption import ArtifactRef
from photo_worker.v2 import exposure_admission as module
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('accepted',[True,False,1,None])
def test_canonical_result_admission_and_owned_params(monkeypatch,accepted):
    edit,_,_=source_fixture();edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    raw=canonical_bytes(edit);pin=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    context=SimpleNamespace(attempt=1,fence=2,result_id=uuid4());tenant,task=uuid4(),uuid4();calls=[]
    monkeypatch.setattr(module,'load_exposure_adoption_params',lambda *a,**k:(pin,edit))
    def execute(sql,args):
        calls.append(args);return SimpleNamespace(fetchone=lambda:(accepted,))
    params,owned,verify=module.sql_exposure_adoption_admission(SimpleNamespace(execute=execute),
        tenant,task,context,SimpleNamespace(read_json=lambda *a:raw),check_active=lambda:None)
    context.fence=99
    assert verify(owned) is (accepted is True)
    assert calls==[(tenant,task,1,2,context.result_id,pin.artifact_id,pin.sha256,raw.decode())]
    changed=owned.model_copy(update={'ev':.75})
    assert verify(changed) is False and len(calls)==1
