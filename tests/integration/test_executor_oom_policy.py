"""Real Executor/TaskDatabase OOM classification; owner-seeded media fixture.

Torch type is a declared lightweight stand-in, not a GPU allocation test.
"""
import json
import sys
from types import SimpleNamespace
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import Executor,Operation
from test_task_leases import queued_task


@pytest.mark.parametrize('kind,code,retryable',[
    ('memory','RESOURCE_OOM',True),
    ('torch-type','RESOURCE_OOM',True),
    ('ordinary','INTERNAL_ERROR',False),
])
def test_executor_classifies_oom_and_releases_actual_resources(database,monkeypatch,kind,code,retryable):
    class DeclaredTorchOOM(RuntimeError):
        pass
    monkeypatch.setitem(sys.modules,'torch',SimpleNamespace(OutOfMemoryError=DeclaredTorchOOM))
    with psycopg.connect(**database) as conn:
        case=queued_task(conn)
    calls=[]
    def operation(ctx):
        calls.append(ctx.lease.command.task_id)
        ctx.check_active()
        error={'memory':MemoryError,'torch-type':DeclaredTorchOOM,'ordinary':RuntimeError}[kind]
        raise error('private diagnostic /path token=must-not-persist')
    executor=Executor(TaskDatabase(make_conninfo(**database)),case['worker'],[Operation('asset.verify','media',operation)])
    assert executor.process(json.dumps(case['command']).encode())=='committed'
    assert calls==[case['task']]
    with psycopg.connect(**database) as conn:
        error,finished=conn.execute('SELECT error,finished_at IS NOT NULL FROM photo.task_attempts WHERE task_id=%s AND attempt=1',(case['task'],)).fetchone()
        assert error==dict(code=code,retryable=retryable,message_key='errors.'+code.lower(),args={},diagnostic_artifact_id=None)
        assert finished
        assert 'private diagnostic' not in json.dumps(error)
        assert conn.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',(case['task'],)).fetchone()==('retry_wait',None)
        assert conn.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(case['task'],)).fetchone()==(0,)
        assert conn.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(case['task'],)).fetchone()==(0,)
        assert conn.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(case['task'],)).fetchone()==(0,)
