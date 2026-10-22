"""Fail before installing/polling shown adopters on missing or unsafe SQL."""
from contextlib import nullcontext
from threading import Event
from types import SimpleNamespace
import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.shown_adoption_schema import REQUIRED_FUNCTIONS,require_shown_adoption_evidence_schema

class Connection:
    autocommit=True
    info=SimpleNamespace(transaction_status=TransactionStatus.IDLE)
    def __init__(self,*,role='photo_scheduler',bad=None,result=None):
        self.role=role;self.bad=bad;self.result=result;self.calls=[]
    def transaction(self):return nullcontext()
    def execute(self,sql,args=()):
        self.calls.append((sql,args))
        value=(self.role,) if sql=='SELECT current_user' else (self.result if args and args[-1]==self.bad else (True,))
        return SimpleNamespace(fetchone=lambda:value)

def test_structural_marker_checks_private_and_exposed_abis():
    db=Connection();require_shown_adoption_evidence_schema(db)
    calls=[(sql,args) for sql,args in db.calls if args]
    assert [args[-1] for _,args in calls]==[signature for signature,_,_ in REQUIRED_FUNCTIONS]
    for sql,args in calls:
        assert 'aclexplode' in sql and 'has_function_privilege' in sql
        assert 'prosecdef' in sql and 'proconfig' in sql and 'pg_get_function_result' in sql
        assert args[1:3]==(args[-1]!=REQUIRED_FUNCTIONS[0][0],)*2

@pytest.mark.parametrize('signature', [row[0] for row in REQUIRED_FUNCTIONS])
@pytest.mark.parametrize('result',[None,(False,)])
def test_missing_or_wrong_authority_fails_closed(signature,result):
    with pytest.raises(ValueError,match='schema unavailable'):
        require_shown_adoption_evidence_schema(Connection(bad=signature,result=result))

def test_wrong_role_and_transaction_state_fail_before_catalog_reads():
    db=Connection(role='photo_worker')
    with pytest.raises(ValueError,match='photo_scheduler'):require_shown_adoption_evidence_schema(db)
    assert len(db.calls)==1
    for autocommit,state in ((False,TransactionStatus.IDLE),(True,TransactionStatus.INTRANS)):
        db=Connection();db.autocommit=autocommit;db.info=SimpleNamespace(transaction_status=state)
        with pytest.raises(ValueError,match='idle autocommit'):require_shown_adoption_evidence_schema(db)
        assert db.calls==[]

@pytest.mark.parametrize('flag',['shown_exposure_enabled','shown_removal_enabled','shown_background_cleanup_enabled','shown_retouch_enabled',None])
@pytest.mark.parametrize('failed_connection',[None,0,1])
def test_startup_mount_checks_both_connections_before_adopter(tmp_path,monkeypatch,flag,failed_connection):
    from photo_worker.v2 import service,shown_adoption_schema
    connections=[object(),object()];pending=list(connections);checked=[];installed=[]
    monkeypatch.setattr(service.psycopg,'connect',lambda *a,**k:nullcontext(pending.pop(0)))
    monkeypatch.setattr(service,'Minio',lambda *a,**k:object())
    from photo_worker.runtime import application_role
    monkeypatch.setattr(application_role,'require_application_role',lambda *a:None)
    def check(connection):
        checked.append(connection)
        if failed_connection is not None and connection is connections[failed_connection]:raise ValueError('schema unavailable')
    monkeypatch.setattr(shown_adoption_schema,'require_shown_adoption_evidence_schema',check)
    monkeypatch.setattr(service,'Adopter',lambda *a,**k:installed.append(k))
    options={} if flag is None else {flag:True}
    config=service.AdopterConfig('unused','https://objects.example','bucket','key','secret',layer_scratch_root=tmp_path,layer_scratch_bytes=1<<20,**options)
    stop=Event();stop.set()
    if flag is not None and failed_connection is not None:
        with pytest.raises(ValueError,match='schema unavailable'):service.run_adopter(config,stop)
        assert installed==[] and checked==connections[:failed_connection+1]
    else:
        service.run_adopter(config,stop)
        assert checked==(connections if flag else []) and len(installed)==1
