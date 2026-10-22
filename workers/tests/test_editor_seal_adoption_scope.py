"""Authenticated scope decoding must fail before artifact reads."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.editor_seal_adoption import load_editor_seal_adoption_inputs


class Connection:
    def __init__(self,row):self.row=row;self.calls=[]
    def execute(self,sql,args):self.calls.append((sql,args));return self
    def fetchone(self):return self.row


@pytest.mark.parametrize('row',[None,(),(uuid4(),'a'*64,{},), (uuid4(),'a'*64,[],uuid4()),(uuid4(),'invalid',{},uuid4()),(uuid4(),'a'*64,{},'not-a-uuid')])
def test_missing_or_malformed_scope_refuses(row):
    connection=Connection(row)
    with pytest.raises(AdoptionInvalid):
        load_editor_seal_adoption_inputs(connection,uuid4(),uuid4(),SimpleNamespace(attempt=1,fence=2,result_id=uuid4()))


def test_scope_is_bound_to_exact_attempt_fence_result():
    tenant,task,result,params,build=(uuid4() for _ in range(5))
    connection=Connection((params,'a'*64,{'backend':'cpu'},build))
    parsed=load_editor_seal_adoption_inputs(connection,tenant,task,SimpleNamespace(attempt=3,fence=9,result_id=result))
    assert parsed.build_run_id==build and parsed.params.artifact_id==params
    assert connection.calls[0][1]==(tenant,task,3,9,result)
