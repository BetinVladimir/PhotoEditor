"""Lifecycle cleanup boundaries without threads, signals or service connections."""
from types import SimpleNamespace
from threading import Event
from uuid import uuid4
import pytest
from photo_worker.runtime import service


@pytest.mark.parametrize('join_fails',[False,True])
def test_abort_failure_still_joins_reporter_and_restores_signals(monkeypatch,join_fails):
    events=[];handlers={};originals={sig:object() for sig in (service.signal.SIGINT,service.signal.SIGTERM)}
    monkeypatch.setattr(service.signal,'getsignal',lambda sig:originals[sig])
    def set_handler(sig,handler):handlers[sig]=handler;events.append(('signal',sig))
    monkeypatch.setattr(service.signal,'signal',set_handler)
    monkeypatch.setattr(service,'_install_metal_budget',lambda config:None)
    class Reporter:
        failed=Event()
        def __init__(self,*args,**kwargs):pass
        def report_once(self):events.append('report')
        def run(self,stop):pytest.fail('real reporter thread started')
    monkeypatch.setattr(service,'CapacityReporter',Reporter)
    class Reports:
        ident=None
        def __init__(self,**kwargs):pass
        def start(self):self.ident=1;events.append('start')
        def join(self,timeout):
            assert timeout==15;events.append('join')
            if join_fails:raise RuntimeError('join failure')
        def is_alive(self):return False
    monkeypatch.setattr(service,'Thread',Reports)
    def abort():events.append('abort');raise RuntimeError('abort failure')
    executor=SimpleNamespace(operations={'asset.verify':object()},abort_active=abort)
    def run(stop):
        assert not stop.is_set();events.append('run')
    transport=SimpleNamespace(executor=executor,run=run)
    database=SimpleNamespace(ping=lambda:events.append('ping'),runtime_operations=lambda *args:frozenset({'asset.verify'}))
    config=SimpleNamespace(capacity_policy=None,resource_class='media',backend='cpu',worker_id=uuid4())
    with pytest.raises(RuntimeError,match='join failure' if join_fails else 'abort failure'):
        service._run_built_worker(config,database,transport)
    assert 'join' in events and events.index('abort')<events.index('join')
    assert all(handlers[sig] is originals[sig] for sig in originals)
