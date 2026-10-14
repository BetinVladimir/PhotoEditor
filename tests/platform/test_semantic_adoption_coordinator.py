from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2 import adoption_service,semantic_replay_inputs
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid


@pytest.mark.parametrize('present',[False,True])
def test_semantic_coordinator_loads_exact_result_params_before_prepare(monkeypatch,present):
    tenant,task,result,root,params=[uuid4() for _ in range(5)];queries=[]
    class Read:
        autocommit=True
        def execute(self,query,args):
            queries.append((query,args))
            if 'v2_adoption_context' in query:
                row=(2,3,result,'semantic.transfer','1',root,'a'*64)
            else:
                assert 'v27_semantic_adoption_params' in query and args==(tenant,task,2,3,result)
                row=(params,'b'*64) if present else None
            return SimpleNamespace(fetchone=lambda:row)
    class ReachedPreparation(Exception):pass
    def prepare(**kwargs):
        assert kwargs['params_ref']==ArtifactRef(params,'b'*64)
        assert kwargs['operation']=='semantic.transfer'
        raise ReachedPreparation()
    monkeypatch.setattr(adoption_service,'prepare_v2_adoption',prepare)
    events=[]
    guards=(ArtifactRef(uuid4(),'c'*64),ArtifactRef(uuid4(),'d'*64))
    monkeypatch.setattr(semantic_replay_inputs,'load_semantic_protection_refs',lambda *args:guards)
    def replay(reader,params_ref,root_ref,**kwargs):
        assert params_ref==ArtifactRef(params,'b'*64) and root_ref==ArtifactRef(root,'a'*64)
        assert kwargs['source_protection_ref']==guards[0] and kwargs['target_protection_ref']==guards[1]
        assert kwargs['max_ram_bytes']==128*1024*1024
        kwargs['verify_authority']('controlled-document');events.append('replay')
    monkeypatch.setattr(semantic_replay_inputs,'verify_pinned_semantic_replay',replay)
    def authority(actual_tenant,actual_task,context,*documents):
        assert (actual_tenant,actual_task,context.result_id)==(tenant,task,result)
        assert documents==('controlled-document',);events.append('authority')
    coordinator=adoption_service.AdoptionCoordinator(Read(),object(),object(),'bucket',
        semantic_authority=authority,semantic_ram_bytes=128*1024*1024,layer_scratch_root=object(),layer_scratch_bytes=65536)
    if present:
        with pytest.raises(ReachedPreparation):coordinator.adopt(tenant,task)
    else:
        with pytest.raises(AdoptionInvalid,match='SEMANTIC_ADOPTION_PARAMS_STALE'):coordinator.adopt(tenant,task)
    assert len(queries)==2
    assert events==(['authority','replay'] if present else [])


@pytest.mark.parametrize('configured',[False,True])
def test_semantic_coordinator_missing_authority_or_budget_denies_before_object_io(configured):
    tenant,task,result,root,params=[uuid4() for _ in range(5)]
    class Read:
        autocommit=True
        def execute(self,query,args):
            row=(1,1,result,'semantic.transfer','1',root,'a'*64) if 'v2_adoption_context' in query else (params,'b'*64)
            return SimpleNamespace(fetchone=lambda:row)
    class Objects:
        def get_object(self,*args,**kwargs):raise AssertionError('Unexpected object IO')
    coordinator=adoption_service.AdoptionCoordinator(Read(),object(),Objects(),'bucket',
        semantic_authority=(lambda *args:None) if configured else None)
    with pytest.raises(AdoptionInvalid,match='SCRATCH_REQUIRED' if configured else 'AUTHORITY_REQUIRED'):
        coordinator.adopt(tenant,task)


@pytest.mark.parametrize('variant',['configured','missing_authority','stale_params'])
def test_semantic_assembly_coordinator_binds_authorities_to_result(monkeypatch,variant):
    tenant,task,result,root,params=[uuid4() for _ in range(5)]
    queries=[];calls=[]
    class Read:
        autocommit=True
        def execute(self,query,args):
            queries.append((query,args))
            if 'v2_adoption_context' in query:
                row=(2,3,result,'v2.semantic.assemble','1',root,'a'*64)
            else:
                assert 'v27_semantic_assembly_adoption_params' in query
                assert args==(tenant,task,2,3,result)
                row=None if variant=='stale_params' else (params,'b'*64)
            return SimpleNamespace(fetchone=lambda:row)
    def authority(actual_tenant,actual_task,context,*documents):
        assert (actual_tenant,actual_task,context.result_id,context.attempt,context.fence)==(tenant,task,result,2,3)
        calls.append(documents)
        return 'snapshot'
    class ReachedPreparation(Exception):pass
    def prepare(**kwargs):
        assert kwargs['params_ref']==ArtifactRef(params,'b'*64)
        assert kwargs['semantic_plan_snapshot']('reader','intent')=='snapshot'
        kwargs['semantic_plan_provenance']('params','intent','snapshot','candidates')
        raise ReachedPreparation()
    monkeypatch.setattr(adoption_service,'prepare_v2_adoption',prepare)
    coordinator=adoption_service.AdoptionCoordinator(Read(),object(),object(),'bucket',
        semantic_plan_snapshot=None if variant=='missing_authority' else authority,
        semantic_plan_provenance=authority)
    if variant=='configured':
        with pytest.raises(ReachedPreparation):coordinator.adopt(tenant,task)
        assert calls==[('reader','intent'),('params','intent','snapshot','candidates')]
    else:
        with pytest.raises(AdoptionInvalid,match='AUTHORITY_REQUIRED' if variant=='missing_authority' else 'PARAMS_STALE'):
            coordinator.adopt(tenant,task)
        assert calls==[]
    assert len(queries)==(1 if variant=='missing_authority' else 2)
