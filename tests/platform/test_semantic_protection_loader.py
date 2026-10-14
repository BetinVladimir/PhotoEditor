from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.semantic_replay_inputs import load_semantic_protection_refs


@pytest.mark.parametrize('variant',['valid','missing','duplicate','transaction'])
def test_semantic_protection_sql_loader_exact_context(variant):
    tenant,task,result,source,target=[uuid4() for _ in range(5)];calls=[]
    row=(source,'a'*64,target,'b'*64)
    def execute(query,args):
        assert 'v27_semantic_adoption_protection' in query and args==(tenant,task,2,3,result)
        calls.append(True)
        return SimpleNamespace(fetchall=lambda:[] if variant=='missing' else [row,row] if variant=='duplicate' else [row])
    connection=SimpleNamespace(autocommit=variant!='transaction',execute=execute)
    def run():return load_semantic_protection_refs(connection,tenant,task,2,3,result)
    if variant=='valid':assert run()==(ArtifactRef(source,'a'*64),ArtifactRef(target,'b'*64))
    elif variant=='transaction':
        with pytest.raises(ValueError,match='AUTOCOMMIT'):run()
        assert calls==[]
    else:
        with pytest.raises(AdoptionInvalid,match='STALE'):run()
