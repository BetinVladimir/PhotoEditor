from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.portrait_donor_adoption_evidence import load_donor_adoption_evidence


@pytest.mark.parametrize('value',[None,{}, {'schema_version':True}, {'unknown':1}])
def test_scheduler_donor_evidence_refuses_missing_or_malformed_context(value):
    tenant,task,result=uuid4(),uuid4(),uuid4();calls=[]
    class Connection:
        autocommit=True
        def execute(self,query,args):
            assert 'v28_donor_adoption_evidence' in query
            assert args==(tenant,task,2,3,result);calls.append('sql');return self
        def fetchone(self):return (value,)
    with pytest.raises(ValueError):load_donor_adoption_evidence(Connection(),tenant,task,
        SimpleNamespace(attempt=2,fence=3,result_id=result),check_active=lambda:None)
    assert calls==['sql']


def test_scheduler_donor_evidence_requires_short_autocommit_reads():
    with pytest.raises(ValueError):load_donor_adoption_evidence(SimpleNamespace(autocommit=False),None,None,None,check_active=lambda:None)


@pytest.mark.parametrize('mode',['ok','alias','extra','order','bool_bytes','changed_sha','vectors_alias'])
def test_scheduler_evidence_validates_original_nested_pins_and_owns_result(mode):
    from hashlib import sha256
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from test_portrait_open_eyes_stage import stage_case
    from test_portrait_donor_qualification import fixture
    params,_,_,_,proof,_=stage_case();profile,*_=fixture()
    task,result=uuid4(),uuid4();body=canonical_bytes(params)
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    manual={name:ArtifactRef(artifact_id=uuid4(),sha256='a'*64).model_dump(mode='json') for name in ('guard_pixel','original_params','root','target_pixel')}
    manual['vectors']=None;pin=profile['donor_qualification']
    objects=[dict(contract=contract,**pin[key],bucket='bucket',object_key=key,object_version='version',bytes=100)
             for contract,key in [('Calibration','calibration'),('Report','report')]]
    value=dict(schema_version=1,run_id=str(uuid4()),task_id=str(task),attempt=1,fence=2,result_id=str(result),
        params_ref=ref.model_dump(mode='json'),params=params.model_dump(mode='json'),proof_jcs=proof.decode(),
        profile=profile,manual=manual,qualification_objects=objects)
    if mode=='alias':manual['root']=pin['report']
    elif mode=='extra':manual['unknown']=True
    elif mode=='order':objects.reverse()
    elif mode=='bool_bytes':objects[0]['bytes']=True
    elif mode=='changed_sha':objects[0]['sha256']='0'*64
    elif mode=='vectors_alias':manual['vectors']=manual['original_params']
    class Connection:
        autocommit=True
        def execute(self,*args):return self
        def fetchone(self):return (value,)
    def run():return load_donor_adoption_evidence(Connection(),uuid4(),task,
        SimpleNamespace(attempt=1,fence=2,result_id=result),check_active=lambda:None)
    if mode=='ok':
        owned=run();manual.clear();objects.clear()
        assert owned['manual']['root'] and len(owned['qualification_objects'])==2
    else:
        with pytest.raises(ValueError):run()


def test_completed_donor_checkpoint_rechecks_owned_context_and_snapshot(monkeypatch):
    from photo_worker.v2 import portrait_donor_adoption_evidence as module
    value={'owned':[1]};context=SimpleNamespace(attempt=1);current={'owned':[1]};calls=[]
    def load(conn,tenant,task,owned,check_active):
        assert owned.attempt==1;calls.append('read');return current
    monkeypatch.setattr(module,'load_donor_adoption_evidence',load)
    check=module.donor_adoption_checkpoint(None,None,None,context,value,check_active=lambda:None)
    context.attempt=2;value['owned'].clear()
    assert check()=={'owned':[1]}
    current['owned'].append(2)
    with pytest.raises(ValueError,match='DONOR_ADOPTION_CONTEXT_CHANGED'):check()
    assert calls==['read','read']
