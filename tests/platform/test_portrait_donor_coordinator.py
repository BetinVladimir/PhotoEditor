from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2 import adoption_service as module
from photo_worker.v2 import portrait_donor_adoption_prepare as donor
from photo_worker.v2.adoption import ArtifactRef,PreparedAdoption
from photo_worker.operations.portrait.donor_adapter import DonorRuntimeOptions
from test_portrait_donor_runtime_lock import current_runtime


@pytest.mark.parametrize('mode',['ok','missing','prepare_denied','commit_false','serialization','unknown_commit','exhausted'])
def test_coordinator_reconstructs_before_tx_and_uses_dedicated_commit(tmp_path,monkeypatch,mode):
    tenant,task,result,root=uuid4(),uuid4(),uuid4(),ArtifactRef(uuid4(),'a'*64);calls=[]
    class Cursor:
        def __init__(self,value):self.value=value
        def fetchone(self):return self.value
    class Read:
        autocommit=True
        def execute(self,query,args):
            assert 'v2_adoption_context' in query
            return Cursor((1,2,result,'portrait.open_eyes','1',root.artifact_id,root.sha256))
    class Write:
        attempts=0
        @contextmanager
        def transaction(self):
            self.attempts+=1
            calls.append('begin')
            yield
            calls.append('commit')
            if mode=='serialization' and self.attempts==1 or mode=='exhausted':
                import psycopg
                raise psycopg.errors.SerializationFailure('conflict')
            if mode=='unknown_commit':
                import psycopg
                raise psycopg.OperationalError('connection lost during commit')
        def execute(self,query,args):
            assert 'adopt_v28_donor' in query and len(args)==8
            assert args[:5]==(tenant,task,1,2,result) and args[-1]=='{"owned":true}'
            calls.append('sql');return Cursor((mode!='commit_false',))
    policy={'owned':True}
    installation=donor.DonorAdoptionInstallation(policy,
        DonorRuntimeOptions(current_runtime().model_copy(deep=True),'b'*64,tmp_path,tmp_path,100,200),tmp_path,300,1<<30)
    def reconstruct(*args,**kwargs):
        assert 'begin' not in calls and kwargs['policy']=={'owned':True}
        calls.append('reconstruct')
        if mode=='prepare_denied':raise ValueError('revoked')
        return donor.PreparedDonorReconstruction(b'{"owned":true}',ArtifactRef(uuid4(),'b'*64),root)
    def inventory(**kwargs):
        assert calls==['reconstruct'];calls.append('inventory')
        return PreparedAdoption((), 'c'*64)
    monkeypatch.setattr(donor,'prepare_donor_reconstruction',reconstruct)
    monkeypatch.setattr(module,'prepare_v2_adoption',inventory)
    monkeypatch.setattr(module.time,'sleep',lambda *args:None)
    coordinator=module.AdoptionCoordinator(Read(),Write(),object(),'bucket',
        donor_installation=None if mode=='missing' else installation)
    policy.clear()
    if mode in ('missing','prepare_denied'):
        with pytest.raises(ValueError):coordinator.adopt(tenant,task)
        assert 'begin' not in calls
    elif mode=='unknown_commit':
        import psycopg
        with pytest.raises(psycopg.OperationalError):coordinator.adopt(tenant,task)
        assert calls==['reconstruct','inventory','begin','sql','commit']
    elif mode=='exhausted':
        with pytest.raises(ValueError,match='RETRYABLE_CONFLICT'):coordinator.adopt(tenant,task)
        assert calls==['reconstruct','inventory']+['begin','sql','commit']*3
    else:
        assert coordinator.adopt(tenant,task) is (mode!='commit_false')
        assert calls==['reconstruct','inventory']+['begin','sql','commit']*(2 if mode=='serialization' else 1)


def test_coordinator_refuses_installation_without_runtime_options(tmp_path):
    installation=donor.DonorAdoptionInstallation({},object(),tmp_path,100,1<<30)
    with pytest.raises(ValueError,match='donor adoption'):
        module.AdoptionCoordinator(SimpleNamespace(autocommit=True),object(),object(),'bucket',donor_installation=installation)
