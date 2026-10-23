"""Operator diagnostics never become client errors or expose private payloads."""
from datetime import datetime, timedelta, timezone
import json
import logging
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import Claim, Lease
from photo_worker.runtime.executor import Executor, Operation
from photo_worker.runtime.gateway import GatewayError
from photo_worker.runtime import diagnostics

SECRET = 'private-photo.cr2 password token SELECT secret /customer/private/path https://private.invalid'


def execute_failure(error, caplog):
    command = TaskCommand(schema_version=1,event_id=uuid4(),tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=1,operation='image.local',operation_version='1',resource_class='cpu',correlation_id=uuid4(),inputs=[],params=dict(artifact_id=uuid4(),sha256='a'*64),model=None,timeout_seconds=600,traceparent='')
    worker = uuid4(); now = datetime.now(timezone.utc)
    lease = Lease(command,worker,1,now+timedelta(seconds=60),now+timedelta(minutes=5),{},'b'*64)
    failures = []; calls = []
    class DB:
        def claim(self,*args): return Claim('claimed',lease)
        def heartbeat(self,*args): raise AssertionError('unexpected slow test')
        def commit_failure(self,actual,error,metrics):
            assert actual is lease
            failures.append(error)
            return 'committed'
        def commit_success(self,*args): raise AssertionError('failure became success')
    def operation(ctx):
        calls.append(1)
        raise error
    with caplog.at_level(logging.WARNING, logger='photo_worker.operator'):
        outcome = Executor(DB(),worker,[Operation('image.local','cpu',operation)]).process(canonical_bytes(command))
    assert outcome == 'committed' and calls == [1] and len(failures) == 1
    return failures[0]


@pytest.mark.parametrize('error,code,retryable',[
    (RuntimeError(SECRET),'INTERNAL_ERROR',False),
    (GatewayError('GATEWAY_RESPONSE_INVALID'),'INTERNAL_ERROR',False),
    (GatewayError('DEPENDENCY_UNAVAILABLE',True),'DEPENDENCY_UNAVAILABLE',True),
    (psycopg.errors.DeadlockDetected(SECRET),'DEPENDENCY_UNAVAILABLE',True),
])
def test_real_executor_emits_only_safe_metadata_preserves_client_disposition(error,code,retryable,caplog):
    result = execute_failure(error, caplog)
    assert result.code == code and result.retryable is retryable and result.args == {}
    records = [json.loads(r.getMessage()) for r in caplog.records if r.name == 'photo_worker.operator']
    assert len(records) == 1 and records[0]['mapped_code'] == code
    record = records[0]
    assert record['event'] == 'worker_failure_metadata' and record['boundary'] == 'operation'
    assert any(f['path']=='photo_worker/runtime/executor.py' and f['function']=='process'
               for e in record['exceptions'] for f in e['frames'])
    serialized = json.dumps(record)
    assert SECRET not in serialized and 'private-photo' not in serialized and str(__file__) not in serialized
    for value in record['exceptions']:
        assert set(value) <= {'module','class','frames','sqlstate'}
        for frame in value['frames']:
            assert set(frame) == {'path','function','line'}
            assert frame['path'].startswith('photo_worker/') and not frame['path'].startswith('/')
    assert all(r.exc_info is None and r.stack_info is None for r in caplog.records)
    if isinstance(error,psycopg.errors.DeadlockDetected):
        assert record['exceptions'][0]['sqlstate'] == '40P01'


def test_does_not_format_unknown_exception_or_read_instance_properties(caplog):
    class PrivateError(Exception):
        def __str__(self): raise AssertionError('message formatting forbidden')
        def __repr__(self): raise AssertionError('repr forbidden')
        @property
        def sqlstate(self): raise AssertionError('instance properties forbidden')
    error = PrivateError(SECRET)
    result = execute_failure(error,caplog)
    record = json.loads(caplog.records[-1].getMessage())
    assert result.code == 'INTERNAL_ERROR'
    assert record['exceptions'][0]['module'] == record['exceptions'][0]['class'] == 'unknown'
    assert SECRET not in json.dumps(record)


def test_diagnostic_logging_failure_cannot_change_commit(monkeypatch,caplog):
    def broken(*args,**kwargs): raise RuntimeError(SECRET)
    monkeypatch.setattr(diagnostics._LOG,'warning',broken)
    result = execute_failure(RuntimeError(SECRET),caplog)
    assert result.code == 'INTERNAL_ERROR' and result.args == {}


def test_closed_boundary_and_codes_and_chain_bounds(caplog):
    with caplog.at_level(logging.WARNING, logger='photo_worker.operator'):
        diagnostics.emit_failure_metadata(RuntimeError(SECRET),SECRET,'operation')
        diagnostics.emit_failure_metadata(RuntimeError(SECRET),'INTERNAL_ERROR',SECRET)
    assert not caplog.records
    error = RuntimeError(SECRET)
    for _ in range(8):
        parent = RuntimeError(SECRET); parent.__cause__ = error; error = parent
    with caplog.at_level(logging.WARNING, logger='photo_worker.operator'):
        diagnostics.emit_failure_metadata(error,'INTERNAL_ERROR','operation')
    record = json.loads(caplog.records[-1].getMessage())
    assert len(record['exceptions']) == 3 and SECRET not in json.dumps(record)


def test_heartbeat_metadata_preserves_lease_loss_authority(caplog):
    from threading import Event
    from photo_worker.runtime.executor import OperationResult
    command=TaskCommand(schema_version=1,event_id=uuid4(),tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=1,operation='image.local',operation_version='1',resource_class='cpu',correlation_id=uuid4(),inputs=[],params=dict(artifact_id=uuid4(),sha256='a'*64),model=None,timeout_seconds=600,traceparent='')
    worker=uuid4();now=datetime.now(timezone.utc);lease=Lease(command,worker,1,now+timedelta(seconds=60),now+timedelta(minutes=5),{},'b'*64)
    class DB:
        def claim(self,*args):return Claim('claimed',lease)
        def heartbeat(self,*args):raise psycopg.errors.InsufficientPrivilege(SECRET)
        def commit_success(self,*args):raise AssertionError('lost lease wrote success')
        def commit_failure(self,*args):raise AssertionError('lost lease wrote failure')
    def operation(ctx):
        assert ctx._lost.wait(1)
        return OperationResult([])
    with caplog.at_level(logging.WARNING,logger='photo_worker.operator'):
        assert Executor(DB(),worker,[Operation('image.local','cpu',operation)],heartbeat_seconds=.001).process(canonical_bytes(command))=='lease_lost'
    records=[json.loads(r.getMessage())for r in caplog.records if r.name=='photo_worker.operator']
    assert len(records)==1 and records[0]['boundary']=='heartbeat' and records[0]['mapped_code']=='LEASE_LOST'
    assert records[0]['exceptions'][0]['sqlstate']=='42501' and SECRET not in json.dumps(records)

def test_exception_slot_access_does_not_invoke_overridden_private_properties(caplog):
    class PrivateError(Exception):
        @property
        def __traceback__(self):raise AssertionError(SECRET)
        @property
        def __cause__(self):raise AssertionError(SECRET)
        @property
        def __context__(self):raise AssertionError(SECRET)
    with caplog.at_level(logging.WARNING,logger='photo_worker.operator'):
        diagnostics.emit_failure_metadata(PrivateError(SECRET),'INTERNAL_ERROR','operation')
    assert len(caplog.records)==1 and SECRET not in caplog.records[0].getMessage()


def test_exported_forged_worker_module_nonexistent_source_is_not_trusted(monkeypatch):
    import sys
    from types import ModuleType
    module=ModuleType('photo_worker.synthetic_diagnostic_review')
    module.__file__=str(diagnostics._ROOT/'synthetic_private_path_review.py')
    monkeypatch.setitem(sys.modules,module.__name__,module)
    exec(compile('def synthetic_private_function():\n raise RuntimeError("private")\n',module.__file__,'exec'),module.__dict__)
    try:module.synthetic_private_function()
    except RuntimeError as error:record=diagnostics._exception_metadata(error)
    assert record['frames']==[]

def test_exported_forged_code_cannot_impersonate_existing_source(monkeypatch):
    module=diagnostics
    source='def forged_private_function():\n raise RuntimeError("private")\n'
    exec(compile(source,module.__file__,'exec'),module.__dict__)
    try:
        try:module.forged_private_function()
        except RuntimeError as error:record=diagnostics._exception_metadata(error)
        assert record['frames']==[]
    finally:del module.__dict__['forged_private_function']

def test_cyclic_exception_chain_stops_without_payload(caplog):
    error=RuntimeError(SECRET);error.__cause__=error
    with caplog.at_level(logging.WARNING,logger='photo_worker.operator'):
        diagnostics.emit_failure_metadata(error,'INTERNAL_ERROR','operation')
    record=json.loads(caplog.records[-1].getMessage())
    assert len(record['exceptions'])==1 and SECRET not in json.dumps(record)
