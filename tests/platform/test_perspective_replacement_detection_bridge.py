from pathlib import Path
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.v2 import perspective_replacement_detection_bridge as module
from photo_worker.v2.adoption import AdoptionInvalid


def client(**kwargs):
    return module.ReplacementDetectionAdoptionBridge(Path('/tmp/photo-job.sock'),'a'*64,
        uuid4(),uuid4(),uuid4(),check_active=kwargs.get('check_active',lambda:None))


def test_bridge_requires_scoped_unix_configuration():
    with pytest.raises(ValueError,match='BRIDGE_CONFIGURATION'):
        module.ReplacementDetectionAdoptionBridge(Path('relative.sock'),'a'*64,
            uuid4(),uuid4(),uuid4(),check_active=lambda:None)


def test_bridge_sends_canonical_scoped_recovery_and_closes_connection(monkeypatch):
    calls=[]
    class Response:
        status=200
        def read(self,limit):return canonical_bytes({'schema_version':1,'adopted':True})
        def getheader(self,name):return 'application/json'
    class Connection:
        def __init__(self,path,timeout):calls.append((path,timeout))
        def request(self,method,route,body,headers):calls.append((method,route,body,headers))
        def getresponse(self):return Response()
        def close(self):calls.append('closed')
    monkeypatch.setattr(module,'_UnixConnection',Connection)
    bridge=client()
    assert bridge.was_adopted() is True and calls[-1]=='closed'
    method,route,body,headers=calls[1]
    packet=parse_json(body,4096)
    assert method=='POST' and route=='/adopted'
    assert packet['tenant_id']==str(bridge.tenant_id) and packet['task_id']==str(bridge.task_id)
    assert packet['attempt']==0 and headers['Authorization']=='Bearer '+'a'*64
    assert b'Bearer' not in body


def test_bridge_recovery_rejects_nonboolean_response(monkeypatch):
    bridge=client()
    monkeypatch.setattr(bridge,'_request',lambda *a,**kw:'true')
    with pytest.raises(AdoptionInvalid,match='RESPONSE_INVALID'):bridge.was_adopted()


def test_bridge_checks_cancellation_before_connect(monkeypatch):
    class Cancelled(Exception):pass
    def cancel():raise Cancelled()
    bridge=client(check_active=cancel)
    monkeypatch.setattr(module,'_UnixConnection',lambda *a:pytest.fail('unexpected connection'))
    with pytest.raises(Cancelled):bridge.was_adopted()
