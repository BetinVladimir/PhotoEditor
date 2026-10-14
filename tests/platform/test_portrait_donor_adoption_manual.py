from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.portrait_donor_adoption_manual import donor_adoption_manual_authority
from test_portrait_open_eyes_stage import stage_case


@pytest.mark.parametrize('mode',['ok','missing','changed_root','changed_params','snapshot','caller_params'])
def test_completed_manual_origin_requires_exact_stored_producer_and_current_snapshot(mode):
    params,*_=stage_case();tenant,task,result=uuid4(),uuid4(),uuid4();calls=[]
    refs={key:ArtifactRef(artifact_id=uuid4(),sha256='a'*64) for key in ('root','original_params','target_pixel','guard_pixel')}
    manual={key:value.model_dump(mode='json') for key,value in refs.items()};manual['vectors']=None
    evidence={'params':params.model_dump(mode='json'),'manual':manual};baseline=deepcopy(evidence)
    row=(refs['root'].artifact_id,refs['root'].sha256,refs['original_params'].artifact_id,refs['original_params'].sha256)
    if mode=='missing':row=None
    elif mode=='changed_root':row=(uuid4(),*row[1:])
    elif mode=='changed_params':row=(*row[:2],uuid4(),row[3])
    class Connection:
        autocommit=True
        def execute(self,query,args):
            assert 'v28_donor_adoption_manual_origin' in query
            assert args[:5]==(tenant,task,1,2,result)
            assert args[5:]==(refs['target_pixel'].artifact_id,'a'*64,refs['guard_pixel'].artifact_id,'a'*64,None,None)
            calls.append('sql');return self
        def fetchone(self):return row
    def checkpoint():
        calls.append('current')
        return {} if mode=='snapshot' else deepcopy(baseline)
    context=SimpleNamespace(attempt=1,fence=2,result_id=result)
    authority=donor_adoption_manual_authority(Connection(),tenant,task,context,evidence,checkpoint=checkpoint,check_active=lambda:None)
    context.attempt=3;evidence.clear()
    if mode=='caller_params':params.strength=0
    def run():return authority(params,refs['root'],refs['original_params'])
    if mode=='ok':assert run() is True and calls==['current','sql','current']
    else:
        with pytest.raises(ValueError):run()
        if mode in ('snapshot','caller_params'):assert calls==['current']
