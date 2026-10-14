from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2 import portrait_open_eyes_manual_stage as stage
from photo_worker.v2 import portrait_donor_qualification_loader as loader
from photo_worker.v2 import portrait_donor_manual_authority as producer_authority


@pytest.mark.parametrize('mode',['ok','load_denied','revoked_after_stage','override'])
def test_sql_manual_stage_owns_inputs_and_qualification(mode,monkeypatch):
    documents=[bytearray(b'{}') for _ in range(10)]
    root=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    producer=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    lease=SimpleNamespace(execution_profile={'owned':True});calls=[]
    def current():
        calls.append('current')
        if mode=='revoked_after_stage':raise ValueError('revoked')
        return {'verified':True}
    def load(database,owned,body,proof,objects,bucket,check_active):
        calls.append('load');assert body==b'{}' and proof==b'{}' and owned.execution_profile=={'owned':True}
        if mode=='load_denied':raise ValueError('denied')
        for doc in documents:doc[0]^=1
        root.sha256='c'*64;producer.sha256='d'*64;lease.execution_profile.clear()
        return b'report',b'calibration',{'policy':True},current
    def execute(database,owned,body,*args,**kwargs):
        calls.append('stage');assert body==b'{}' and owned.execution_profile=={'owned':True}
        assert args[:7]==(b'{}',)*7 and args[8]==b'{}' and args[10]==b'{}'
        assert args[7].sha256=='a'*64 and args[9].sha256=='b'*64
        assert kwargs['report_raw']==b'report' and kwargs['calibration_raw']==b'calibration'
        return 'result'
    monkeypatch.setattr(producer_authority,'donor_manual_authority',lambda *args:lambda *args:True)
    monkeypatch.setattr(loader,'load_donor_qualification',load)
    monkeypatch.setattr(stage,'prepare_and_stage_leased_manual_open_eyes',execute)
    options={'report_raw':b'override'} if mode=='override' else {}
    def run():
        return stage.prepare_and_stage_sql_manual_open_eyes(None,lease,documents[0],*documents[1:8],root,documents[8],producer,documents[9],None,None,None,
            qualification_objects=None,bucket='test',check_active=lambda:None,**options)
    if mode=='ok':assert run()=='result' and calls==['load','stage','current']
    else:
        with pytest.raises(ValueError):run()
        if mode=='override':assert calls==[]
        if mode=='load_denied':assert calls==['load']
        if mode=='revoked_after_stage':assert calls==['load','stage','current']


@pytest.mark.parametrize('vectors',[0,True,'{}',{},b'',b'x',b'x'*((1<<20)+1)])
def test_sql_stage_rejects_invalid_vectors_before_authority(vectors,monkeypatch):
    def forbidden(*args,**kwargs):raise AssertionError('invalid vectors reached authority')
    monkeypatch.setattr(producer_authority,'donor_manual_authority',forbidden)
    monkeypatch.setattr(loader,'load_donor_qualification',forbidden)
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    docs=[b'{}']*10
    with pytest.raises(ValueError,match='DONOR_DOCUMENT_INVALID'):
        stage.prepare_and_stage_sql_manual_open_eyes(None,SimpleNamespace(execution_profile={}),docs[0],*docs[1:8],ref,docs[8],ref,docs[9],None,None,None,
            vectors_raw=vectors,qualification_objects=None,bucket='test',check_active=lambda:None)
