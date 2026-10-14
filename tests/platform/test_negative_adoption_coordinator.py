from contextlib import nullcontext
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2 import adoption_service,negative_adoption_prepare
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.operations.optical.negative_adapter import NegativeRuntimeOptions


@pytest.mark.parametrize('mode',['ok','unconfigured','stale','replay_refused'])
def test_negative_coordinator_uses_dedicated_commit_after_replay(monkeypatch,tmp_path,mode):
    tenant,task,result,root,params=[uuid4() for _ in range(5)];events=[]
    class Read:
        autocommit=True
        def execute(self,query,args):
            if 'v2_adoption_context' in query:row=(1,2,result,'optical.negative','1',root,'a'*64)
            else:
                assert 'v29_negative_adoption_params' in query and args==(tenant,task,1,2,result)
                row=None if mode=='stale' else (params,'b'*64)
            return SimpleNamespace(fetchone=lambda:row)
    class Write:
        def transaction(self):events.append('transaction');return nullcontext()
        def execute(self,query,args):
            assert 'adopt_v29_negative' in query and len(args)==9
            assert args[-2:]==('{}','{}')
            events.append('commit');return SimpleNamespace(fetchone=lambda:(True,))
    def reconstruct(*args,**kwargs):
        assert args[5]==ArtifactRef(params,'b'*64)
        events.append('replay')
        if mode=='replay_refused':raise AdoptionInvalid('NEGATIVE_REPLAY_MISMATCH')
        return negative_adoption_prepare.PreparedNegativeReconstruction(b'{}',b'{}',args[5],ArtifactRef(root,'a'*64))
    monkeypatch.setattr(negative_adoption_prepare,'prepare_negative_reconstruction',reconstruct)
    monkeypatch.setattr(adoption_service,'prepare_v2_adoption',lambda **kwargs:SimpleNamespace(inventory=(),digest='c'*64))
    installation=negative_adoption_prepare.NegativeAdoptionInstallation({},NegativeRuntimeOptions(object(),'d'*64,100,1000),tmp_path,1000,lambda image:True)
    coordinator=adoption_service.AdoptionCoordinator(Read(),Write(),object(),'bucket',
        negative_installation=None if mode=='unconfigured' else installation)
    if mode=='ok':assert coordinator.adopt(tenant,task) is True and events==['replay','transaction','commit']
    else:
        with pytest.raises(AdoptionInvalid):coordinator.adopt(tenant,task)
        assert events==(['replay'] if mode=='replay_refused' else [])
