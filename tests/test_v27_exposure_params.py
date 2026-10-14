from types import SimpleNamespace
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence,AdoptionInvalid
from photo_worker.v2.exposure_params import load_exposure_adoption_params
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('failure',[None,'missing','hash','source','duplicate'])
def test_result_scoped_reader(failure):
    edit,_,_=source_fixture();raw=canonical_bytes(edit)
    if failure=='duplicate':raw=b'{"schema_version":1,'+raw[1:]
    ref=ArtifactRef(uuid4(),sha256(raw).hexdigest() if failure!='hash' else 'f'*64)
    context=SimpleNamespace(operation='semantic.local_exposure',version='1',attempt=2,fence=3,result_id=uuid4())
    tenant,task=uuid4(),uuid4();calls=[]
    def execute(sql,args):
        calls.append(args)
        return SimpleNamespace(fetchone=lambda:None if failure=='missing' else (ref.artifact_id,ref.sha256))
    db=SimpleNamespace(autocommit=True,execute=execute)
    reader=SimpleNamespace(evidence=lambda value:ArtifactEvidence(value,'application/json',len(raw),None,
        'output' if failure=='source' else 'input'),read_json=lambda *args:raw)
    if failure:
        with pytest.raises(AdoptionInvalid):load_exposure_adoption_params(db,tenant,task,context,reader,check_active=lambda:None)
    else:
        actual,value=load_exposure_adoption_params(db,tenant,task,context,reader,check_active=lambda:None)
        assert actual==ref and value.kind=='local_exposure'
    assert calls==[(tenant,task,2,3,context.result_id)]
