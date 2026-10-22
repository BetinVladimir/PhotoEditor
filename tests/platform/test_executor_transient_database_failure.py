"""Full Executor control flow with the real bounded TaskDatabase query retry."""
from datetime import datetime,timedelta,timezone
from uuid import uuid4
import json
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import TaskDatabase,Claim,Lease
from photo_worker.runtime.executor import Executor,Operation

@pytest.mark.parametrize('error_type,expected,retryable,query_attempts',[
 (psycopg.errors.DeadlockDetected,'DEPENDENCY_UNAVAILABLE',True,3),
 (psycopg.errors.SerializationFailure,'DEPENDENCY_UNAVAILABLE',True,3),
 (psycopg.errors.InsufficientPrivilege,'INTERNAL_ERROR',False,1),
 (psycopg.errors.CheckViolation,'INTERNAL_ERROR',False,1),
 (RuntimeError,'INTERNAL_ERROR',False,1),
])
def test_exhausted_db_conflicts_get_normal_task_recovery_not_inline_reexecution(error_type,expected,retryable,query_attempts):
 command=TaskCommand(schema_version=1,event_id=uuid4(),tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=1,operation='asset.verify',operation_version='1',resource_class='media',correlation_id=uuid4(),inputs=[],params=dict(artifact_id=uuid4(),sha256='a'*64),model=None,timeout_seconds=600,traceparent='')
 worker=uuid4();now=datetime.now(timezone.utc);lease=Lease(command,worker,1,now+timedelta(seconds=60),now+timedelta(minutes=5),{},'b'*64)
 secret='password-secret SQL tenant/private/path'
 attempts=[];executions=[];failures=[]
 class FailedConnection:
  def __enter__(self):attempts.append(1);raise error_type(secret)
  def __exit__(self,*args):return False
 source=TaskDatabase('not-used-no-connection');source._connect=lambda:FailedConnection()
 class DB:
  def claim(self,*args):return Claim('claimed',lease)
  def heartbeat(self,*args):raise AssertionError('unexpected slow test')
  def commit_failure(self,actual,error,metrics):assert actual==lease;failures.append(error);return 'committed'
  def commit_success(self,*args):raise AssertionError('failed operation committed success')
 def operation(ctx):executions.append(ctx.lease.command.task_id);ctx.check_active();source._query('not logged',())
 executor=Executor(DB(),worker,[Operation('asset.verify','media',operation)])
 assert executor.process(canonical_bytes(command))=='committed'
 assert executions==[command.task_id] and len(attempts)==query_attempts and len(failures)==1
 error=failures[0];assert error.code==expected and error.retryable is retryable and error.args=={}
 assert secret not in json.dumps(error.model_dump(mode='json'))
