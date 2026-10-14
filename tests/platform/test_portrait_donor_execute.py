from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.portrait.local_result import StagedLocalPortrait
from photo_worker.v2 import portrait_open_eyes_execute as module
from test_portrait_open_eyes_stage import stage_case
from test_portrait_donor_qualification import fixture


@pytest.mark.parametrize('denial',[None,'qualification','attempt','source','manual'])
def test_sql_handoff_repeats_authority_and_refuses_revocation(monkeypatch,denial):
    params,*_=stage_case();body=canonical_bytes(params)
    profile,*_=fixture();lease=SimpleNamespace(execution_profile=deepcopy(profile))
    root=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    producer=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    outputs=tuple(ArtifactRef(artifact_id=uuid4(),sha256='c'*64) for _ in range(6))
    staged=StagedLocalPortrait(outputs[-1],outputs,True,100)
    calls=[]
    def stage(*args,**kwargs):
        calls.append('stage')
        assert args[2]==body
        lease.execution_profile.clear()
        return staged
    def load(*args,**kwargs):
        calls.append('load')
        assert args[1].execution_profile==profile
        def current():
            calls.append('qualification')
            if denial=='qualification':raise ValueError('revoked')
            return profile['donor_qualification']
        return None,None,None,current
    def attempt_factory(db,owned,raw,qualified,active):
        def check():
            calls.append('attempt');active()
            if denial=='attempt':raise ValueError('revoked')
            qualified()
        return check
    def authority(label):
        def factory(*args):
            def check(*values):
                calls.append(label)
                return denial!=label
            return check
        return factory
    monkeypatch.setattr(module,'prepare_and_stage_sql_manual_open_eyes',stage)
    monkeypatch.setattr(module,'load_donor_qualification',load)
    monkeypatch.setattr(module,'donor_attempt_checkpoint',attempt_factory)
    monkeypatch.setattr(module,'donor_source_authority',authority('source'))
    monkeypatch.setattr(module,'donor_manual_authority',authority('manual'))
    def run():return module.execute_sql_manual_open_eyes(None,lease,body,*([b'{}']*7),root,b'{}',producer,b'{}',None,None,None,
        qualification_objects=None,bucket='test',check_active=lambda:None,
        declared_inputs=[root,producer],output_budget=100)
    if denial is None:
        result=run()
        assert result.outputs==[outputs[-1]] and result.review_required is True
        assert calls==['stage','load']+['attempt','qualification','source','manual','attempt','qualification']*2
    else:
        with pytest.raises(ValueError):run()
        assert calls[:2]==['stage','load']


def test_declared_adapter_uses_verified_index_and_leased_output_budget(monkeypatch):
    from photo_worker.v2 import portrait_donor_input_documents as loader
    root=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    documents=SimpleNamespace(**{key:b'{}' for key in ('params','source','donor','confirmation','source_faces','donor_faces','target','protected','root','producer')},root_ref=root,producer_ref=root,vectors=None)
    lease=SimpleNamespace(command=SimpleNamespace(params=root),execution_profile={'resource_estimate':{'output_bytes':123}})
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    class Artifacts:
        scratch=object()
        def declared_input_refs(self,maximum):
            assert maximum==512
            return (root,)
    artifacts=Artifacts()
    monkeypatch.setattr(loader,'load_donor_input_documents',lambda a,p,check_active:documents)
    def execute(*args,**kwargs):
        assert args[0]=='database' and args[1] is lease
        assert args[-2] is artifacts and args[-1] is artifacts.scratch
        assert kwargs['output_budget']==123 and kwargs['declared_inputs']==(root,)
        assert kwargs['vectors_raw'] is None
        return 'result'
    monkeypatch.setattr(module,'execute_sql_manual_open_eyes',execute)
    assert module.execute_declared_sql_open_eyes(ctx,artifacts,'database',object(),qualification_objects=None,bucket='bucket')=='result'
    with pytest.raises(ValueError,match='DONOR_LEASE_INPUT_OVERRIDE'):
        module.execute_declared_sql_open_eyes(ctx,artifacts,'database',object(),qualification_objects=None,bucket='bucket',output_budget=999)


def test_declared_adapter_constructs_worker_catalog_reader(monkeypatch):
    from photo_worker.v2 import portrait_donor_input_documents as loader
    from photo_worker.v2 import portrait_donor_object_reader as catalog
    root=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    documents=SimpleNamespace(**{key:b'{}' for key in ('params','source','donor','confirmation','source_faces','donor_faces','target','protected','root','producer')},root_ref=root,producer_ref=root,vectors=None)
    lease=SimpleNamespace(command=SimpleNamespace(params=root),execution_profile={'resource_estimate':{'output_bytes':123}})
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    artifacts=SimpleNamespace(scratch=object(),declared_input_refs=lambda maximum:(root,))
    objects=object();seen=[]
    monkeypatch.setattr(loader,'load_donor_input_documents',lambda *args,**kwargs:documents)
    def factory(database,owned,body,proof,store,**kwargs):
        assert (database,owned,body,proof,store)==('db',lease,b'{}',b'{}',objects)
        assert kwargs['bucket']=='bucket';seen.append('catalog')
        return object()
    monkeypatch.setattr(catalog,'DonorWorkerObjectReader',factory)
    monkeypatch.setattr(module,'execute_sql_manual_open_eyes',lambda *args,**kwargs:'result')
    assert module.execute_declared_sql_open_eyes(ctx,artifacts,'db',qualification_objects=objects,bucket='bucket')=='result'
    assert seen==['catalog']
