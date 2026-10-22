"""Controlled SQL-return witnesses; no live qualification or task claims."""
import copy,json
from pathlib import Path
from types import SimpleNamespace as NS
from uuid import UUID
import pytest
from photo_worker.v2.authored_render_admission import decode_qualification,SQLAuthoredRenderQualification
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure


def fixture():
 raw=(Path(__file__).parent/'fixtures/authored_render/final.json').read_bytes()
 import hashlib
 body=json.loads(raw)
 c=NS(operation='render.compose',operation_version='2',tenant_id=UUID(body['tenant_id']),run_id=UUID(int=32),task_id=UUID(int=33),attempt=1,resource_class='gpu',params=NS(artifact_id=UUID(int=34),sha256=hashlib.sha256(raw).hexdigest()))
 lease=NS(command=c,fence=2,worker_id=UUID(int=35),input_digest='5'*64)
 value=dict(schema_version=1,operation=c.operation,operation_version=c.operation_version,tenant_id=str(c.tenant_id),run_id=str(c.run_id),task_id=str(c.task_id),worker_id=str(lease.worker_id),attempt=1,fence=2,params=dict(artifact_id=str(c.params.artifact_id),sha256=c.params.sha256),input_digest=lease.input_digest,qualification_pin=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,backend='cpu',device_profile_sha='c'*64,dataset_sha='d'*64,calibration_sha='9'*64))
 return raw,lease,value


def test_exact_actual_pair_witness_and_real_database_query_port():
 raw,lease,value=fixture();db=object.__new__(TaskDatabase);calls=[]
 def query(sql,args):calls.append((sql,args));return (value,)
 db._query=query;checks=[];ctx=NS(lease=lease,check_active=lambda:checks.append(True))
 result=SQLAuthoredRenderQualification(db).qualification(ctx,raw)
 assert result.operation=='render.compose' and result.operation_version=='2'
 assert len(checks)==2 and len(calls)==1
 assert calls[0][1]==(lease.command.tenant_id,lease.command.run_id,lease.command.task_id,1,2,lease.worker_id,lease.command.params.artifact_id,lease.command.params.sha256,raw.decode(),lease.input_digest)


@pytest.mark.parametrize('field,value',[('schema_version',True),('attempt',True),('fence',True),('operation_version','1'),('operation','render.authored_segment'),('tenant_id',str(UUID(int=99))),('task_id',str(UUID(int=99))),('worker_id',str(UUID(int=99))),('run_id',str(UUID(int=99))),('input_digest','0'*64),('params',{}),('extra',1)])
def test_forged_or_cross_pair_witness_refused(field,value):
 _,lease,w=fixture();w[field]=value
 with pytest.raises(ValueError):decode_qualification(w,lease)


def test_v1_is_not_reinterpreted_and_invalid_raw_never_queries():
 raw,lease,w=fixture();db=object.__new__(TaskDatabase)
 db._query=lambda *args:pytest.fail('SQL must not be reached')
 ctx=NS(lease=lease,check_active=lambda:None)
 lease.command.operation_version='1'
 with pytest.raises(OperationFailure):SQLAuthoredRenderQualification(db).qualification(ctx,raw)
 lease.command.operation_version='2'
 with pytest.raises(OperationFailure):SQLAuthoredRenderQualification(db).qualification(ctx,b'{}')


def test_database_failures_not_relabelled_as_input_and_expiry_rechecked():
 raw,lease,w=fixture();db=object.__new__(TaskDatabase)
 def failure(*a):raise TimeoutError('controlled')
 db._query=failure;ctx=NS(lease=lease,check_active=lambda:None)
 with pytest.raises(TimeoutError):SQLAuthoredRenderQualification(db).qualification(ctx,raw)
 checks=[];db._query=lambda *a:(w,)
 def active():
  checks.append(1)
  if len(checks)==2:raise TimeoutError('lease lost')
 ctx.check_active=active
 with pytest.raises(TimeoutError):SQLAuthoredRenderQualification(db).qualification(ctx,raw)
