"""Isolated candidate only: real safe emitter, no worker/database/timers/threads."""
from pathlib import Path
from threading import Event
import builtins
import importlib.util
import json
import logging
import os
import sys
import pytest

D=Path(__file__).resolve().parent
REPO=D.parents[1]
sys.path.insert(0,str(REPO/'workers'))
from photo_worker.runtime import diagnostics
SECRET='https://user:password@private.invalid/db?token=secret SELECT private customer/path'

@pytest.fixture
def capacity():
    source=Path(os.environ.get('CAPACITY_DIAGNOSTICS_TEST_SOURCE',str(REPO/'workers/photo_worker/runtime/capacity.py')))
    name='capacity_safe_diagnostics_candidate'
    spec=importlib.util.spec_from_file_location(name,source)
    module=importlib.util.module_from_spec(spec);sys.modules[name]=module
    try:
        spec.loader.exec_module(module)
        yield module
    finally:sys.modules.pop(name,None)

class Tick:
    def __init__(self):self.stopped=Event();self.set_calls=0
    def wait(self,interval):return self.stopped.is_set()
    def set(self):self.set_calls+=1;self.stopped.set()
    def is_set(self):return self.stopped.is_set()

def failing(capacity,phase,abort=None):
    stop=Tick();calls=[]
    def sample():
        calls.append('sample')
        if phase=='sample':raise RuntimeError(SECRET)
        return capacity.Capacity(1,2,3)
    def report(value):
        calls.append('report')
        if phase=='report':raise RuntimeError(SECRET)
        return False
    def stopped_abort():
        assert reporter.failed.is_set() and stop.is_set()
        calls.append('abort')
        if abort:abort()
    reporter=capacity.CapacityReporter(report,sample,stopped_abort)
    return reporter,stop,calls

@pytest.mark.parametrize('phase',['sample','report','rejected'])
def test_real_emitter_safe_class_boundary_and_once_shutdown(capacity,caplog,phase):
    reporter,stop,calls=failing(capacity,phase)
    with caplog.at_level(logging.WARNING,logger='photo_worker.operator'):
        reporter.run(stop)
    rows=[json.loads(r.getMessage())for r in caplog.records if r.name=='photo_worker.operator']
    assert len(rows)==1
    row=rows[0]
    assert row['event']=='worker_failure_metadata' and row['mapped_code']=='INTERNAL_ERROR' and row['boundary']=='heartbeat'
    assert row['exceptions'][0]['module']=='builtins' and row['exceptions'][0]['class']=='RuntimeError'
    assert SECRET not in json.dumps(rows) and 'password' not in json.dumps(rows)
    assert all(r.exc_info is None and r.stack_info is None for r in caplog.records)
    assert stop.is_set() and reporter.failed.is_set() and stop.set_calls==1 and calls.count('abort')==1
    assert calls==(['sample','abort']if phase=='sample'else['sample','report','abort'])

@pytest.mark.parametrize('error',[RuntimeError(SECRET),SystemExit(7)])
def test_emitter_failure_still_aborts_with_flags_already_set(capacity,monkeypatch,error):
    reporter,stop,calls=failing(capacity,'report');seen=[]
    def broken(*args):
        assert reporter.failed.is_set() and stop.is_set()
        seen.append(args[1:]);raise error
    monkeypatch.setattr(diagnostics,'emit_failure_metadata',broken)
    reporter.run(stop)
    assert seen==[('INTERNAL_ERROR','heartbeat')] and stop.set_calls==1 and calls.count('abort')==1

def test_diagnostic_import_failure_cannot_prevent_abort(capacity,monkeypatch):
    reporter,stop,calls=failing(capacity,'report');original=builtins.__import__;seen=[]
    def importing(name,*args,**kwargs):
        if name=='photo_worker.runtime.diagnostics':seen.append(name);raise ImportError(SECRET)
        return original(name,*args,**kwargs)
    monkeypatch.setattr(builtins,'__import__',importing)
    reporter.run(stop)
    assert len(seen)==1 and reporter.failed.is_set() and stop.set_calls==1 and calls.count('abort')==1

def test_actual_logger_failure_cannot_prevent_abort(capacity,monkeypatch):
    reporter,stop,calls=failing(capacity,'report');seen=[]
    def broken(*args,**kwargs):seen.append(1);raise OSError(SECRET)
    monkeypatch.setattr(diagnostics._LOG,'warning',broken)
    reporter.run(stop)
    assert seen==[1] and reporter.failed.is_set() and stop.set_calls==1 and calls.count('abort')==1

def test_abort_failure_remains_original_and_called_once(capacity,monkeypatch):
    expected=RuntimeError('abort-failure')
    def abort():raise expected
    reporter,stop,calls=failing(capacity,'report',abort)
    monkeypatch.setattr(diagnostics,'emit_failure_metadata',lambda *args:None)
    with pytest.raises(RuntimeError)as raised:reporter.run(stop)
    assert raised.value is expected and calls.count('abort')==1 and reporter.failed.is_set() and stop.set_calls==1

def test_already_stopped_emits_nothing_and_does_not_abort(capacity,monkeypatch):
    seen=[];monkeypatch.setattr(diagnostics,'emit_failure_metadata',lambda *a:seen.append(a))
    reporter,stop,calls=failing(capacity,'report');stop.set();reporter.run(stop)
    assert not seen and not calls and not reporter.failed.is_set()
