"""Real gate adapter sequence with controlled DB returns; no qualification claim."""
import json
from pathlib import Path
from types import SimpleNamespace as NS
from uuid import UUID
from hashlib import sha256
import pytest
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2 import authored_render_live_scope as m


def fixture(kind='final'):
 r=Path(__file__).resolve().parents[2]
 raw=(r/'tests/contracts/fixtures/authored_render'/f'{kind}.json').read_bytes();b=json.loads(raw)
 pair={'final':('render.compose','2'),'segment':('render.authored_segment','1'),'geometry':('render.authored_geometry','1')}[kind]
 c=NS(operation=pair[0],operation_version=pair[1],tenant_id=UUID(b['tenant_id']),run_id=UUID(int=32),task_id=UUID(int=33),resource_class='gpu',attempt=1,params=NS(artifact_id=UUID(int=34),sha256=sha256(raw).hexdigest()))
 pin=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,backend='cpu',device_profile_sha='c'*64,dataset_sha='d'*64,calibration_sha='9'*64)
 lease=NS(command=c,fence=2,worker_id=UUID(int=35),input_digest='5'*64,execution_profile=dict(operation=pair[0],operation_version=pair[1],native_qualification_pin=pin))
 value=dict(schema_version=1,operation=c.operation,operation_version=c.operation_version,tenant_id=str(c.tenant_id),run_id=str(c.run_id),task_id=str(c.task_id),worker_id=str(lease.worker_id),attempt=1,fence=2,params=dict(artifact_id=str(c.params.artifact_id),sha256=c.params.sha256),input_digest=lease.input_digest,qualification_pin=pin)
 calls=[];db=object.__new__(TaskDatabase)
 def query(sql,args):
  calls.append((sql,args));return (value,) if 'worker_qualification' in sql else (True,)
 db._query=query
 return raw,NS(lease=lease,check_active=lambda:None),db,calls


@pytest.mark.parametrize('kind',['final','segment','geometry'])
def test_actual_source_gate_and_pair_qualification_before_and_after_compute(kind):
 raw,ctx,db,calls=fixture(kind);g=m.SQLAuthoredLiveScope(db)
 with g.current(ctx,raw) as w:
  assert len(calls)==2;assert calls[0][0]==m.SQL[(ctx.lease.command.operation,ctx.lease.command.operation_version)]
  assert calls[0][1][8]==raw.decode()
 assert len(calls)==4 and calls[2:]==calls[:2]


@pytest.mark.parametrize('row',[None,(),(False,),(1,),('true',),(True,True)])
def test_no_content_or_compute_after_failed_source_gate(row):
 raw,ctx,db,calls=fixture();db._query=lambda *a:row;entered=[]
 with pytest.raises(OperationFailure):
  with m.SQLAuthoredLiveScope(db).current(ctx,raw):entered.append(1)
 assert not entered


def test_late_revocation_cannot_return_success():
 raw,ctx,db,calls=fixture();original=db._query
 def query(sql,args):
  if len(calls)>=2:return (False,)
  return original(sql,args)
 db._query=query
 with pytest.raises(OperationFailure):
  with m.SQLAuthoredLiveScope(db).current(ctx,raw):pass


def test_original_failure_and_late_scope_failure_both_retained():
 raw,ctx,db,calls=fixture();original=db._query
 db._query=lambda sql,args:(False,) if len(calls)>=2 else original(sql,args)
 with pytest.raises(BaseExceptionGroup) as e:
  with m.SQLAuthoredLiveScope(db).current(ctx,raw):raise RuntimeError('controlled original')
 assert isinstance(e.value.exceptions[0],RuntimeError)
 assert isinstance(e.value.exceptions[1],OperationFailure)


def test_legacy_v1_refused_before_any_db_io():
 raw,ctx,db,calls=fixture();ctx.lease.command.operation_version='1'
 with pytest.raises(OperationFailure):m.SQLAuthoredLiveScope(db).admit(ctx,raw)
 assert not calls
