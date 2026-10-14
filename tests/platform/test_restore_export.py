from types import SimpleNamespace as NS
from uuid import uuid4,UUID
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime import restore_export as m


@pytest.mark.parametrize('mode',['valid','zero_job','extra','status','redirect','network','scope','inactive','fixture_denied'])
def test_native_export_ticket_is_not_completed_download_or_replay_proof(monkeypatch,mode):
    target=m.RestoreExportTarget('https://restore.test',uuid4(),uuid4(),uuid4(),'fresh','Bearer private')
    value={'download_job_id':str(UUID(int=0) if mode=='zero_job' else uuid4()),'status':'completed' if mode=='status' else 'queued'}
    if mode=='extra':value['extra']=True
    calls=[]
    class Connection:
        def __init__(self,*a,**k):assert k['timeout']==5
        def request(self,method,path,body,headers):
            calls.append(path)
            assert method=='POST' and path==f'/api/v1/tenants/{target.tenant_id}/download-jobs'
            assert parse_json(body)=={'selection_snapshot_id':str(target.selection_snapshot_id),'preset_id':str(target.preset_id)}
            if mode=='network':raise OSError('private')
        def getresponse(self):return NS(status=302 if mode=='redirect' else 202,read=lambda _:canonical_bytes(value),
            getheader=lambda name,default=None:'application/json' if name=='Content-Type' else default)
        def close(self):pass
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    def scope(*a):
        if mode=='scope':raise ValueError('isolation lost')
        if mode=='fixture_denied':return False
    def invoke():return m.request_fixture_export(target,tls_context=ssl.create_default_context(),assert_fixture_scope=scope,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert str(invoke().download_job_id)==value['download_job_id']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert len(calls)==(0 if mode in ('scope','inactive','fixture_denied') else 1)
