from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.person import PersonParams
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2 import relight_adoption_person as module
from test_relight_inputs import inputs


@pytest.mark.parametrize('mode',['','source','sha','stale','changed','size'])
def test_completed_original_person_params_and_repeated_origin(monkeypatch,mode):
    params,_,_,person,*_=inputs();producer=PersonParams(image=params.base.source,model_lock_id='a'*64)
    if mode=='source':producer=producer.model_copy(update={'image':ArtifactRef(artifact_id=uuid4(),sha256='a'*64)})
    raw=canonical_bytes(producer)
    record=dict(artifact_id=str(uuid4()),sha256=canonical_hash(producer),bucket='test',object_key='person-params',object_version='v1',bytes=len(raw),media_type='application/json')
    if mode=='size':record['bytes']+=1
    calls=[]
    def execute(sql,args):
        calls.append(args);value=dict(record)
        if mode=='changed' and len(calls)>1:value['object_version']='v2'
        return SimpleNamespace(fetchone=lambda:(None if mode=='stale' else value,))
    monkeypatch.setattr(module,'load_relight_adoption_source',lambda *args,**kwargs:{'source':'fixture'})
    monkeypatch.setattr(module,'relight_adoption_source_checkpoint',lambda *args,**kwargs:lambda:None)
    class Reader:
        def evidence(self,ref):return ArtifactEvidence(ref,'application/json',len(raw),None,'input')
        def read_json(self,ref,maximum):return b'x'+raw[1:] if mode=='sha' else raw
    ctx=SimpleNamespace(attempt=1,fence=1,result_id=uuid4())
    call=lambda:module.verify_relight_adoption_person(SimpleNamespace(execute=execute),Reader(),uuid4(),uuid4(),ctx,params,person,check_active=lambda:None)
    if mode:
        with pytest.raises(ValueError):call()
    else:assert call() is True and len(calls)==2
