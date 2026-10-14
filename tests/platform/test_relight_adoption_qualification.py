from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.v2 import relight_adoption_qualification as module
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.contracts.base import canonical_bytes
from test_relight_inputs import inputs


@pytest.mark.parametrize('mode',['ok','bytes','stale','changed','calibration'])
def test_completed_qualification_pinned_bytes_and_requery(monkeypatch,mode):
    params,_,_,_,_,calibration,*_=inputs()
    raw=canonical_bytes(calibration); report=b'{}'
    records={};blobs={}
    for label,payload in [('report',report),('calibration',raw)]:
        aid=str(uuid4());digest=sha256(payload).hexdigest()
        records[label]=dict(artifact_id=aid,sha256=digest,bucket='test',object_key=label,object_version='v1',bytes=len(payload),media_type='application/json')
        blobs[aid]=payload
    record=dict(pin={},policy={},**records);calls=[];verified=[]
    def execute(*a):
        calls.append(1);value=dict(record)
        if mode=='changed' and len(calls)>1:value['policy']={'changed':True}
        return SimpleNamespace(fetchone=lambda:(None if mode=='stale' else value,))
    def verify(*a,**k):
        verified.append(a)
        return None,calibration.model_copy(update={'depth_sigma':.2}) if mode=='calibration' else calibration
    monkeypatch.setattr(module,'verify_relight_qualification',verify)
    class Reader:
        def evidence(self,ref):return ArtifactEvidence(ref,'application/json',len(blobs[str(ref.artifact_id)]),None,'input')
        def read_json(self,ref,maximum):
            payload=blobs[str(ref.artifact_id)]
            return b'x'+payload[1:] if mode=='bytes' else payload
    call=lambda:module.verify_completed_relight_qualification(SimpleNamespace(autocommit=True,execute=execute),Reader(),uuid4(),uuid4(),
        SimpleNamespace(attempt=1,fence=1,result_id=uuid4()),params,calibration,expected_runtime_sha='a'*64,check_active=lambda:None)
    if mode=='ok':assert call() is True and len(calls)==2 and len(verified)==1
    else:
        with pytest.raises(ValueError):call()
