from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime.transport import Transport

def test_transport_consumes_only_its_worker_queue():
    worker=uuid4();executor=SimpleNamespace(worker_id=worker)
    transport=Transport('amqp://localhost', 'media',executor,development=True)
    assert transport.queue==f'q.worker.{worker}.media'
    legacy=Transport('amqp://localhost','media',executor,development=True,targeted=False)
    assert legacy.queue=='q.worker.media'
    with pytest.raises(ValueError):Transport('amqp://localhost','media',SimpleNamespace(worker_id='arbitrary'),development=True)


def test_stopped_transport_does_not_open_connection(monkeypatch):
    from threading import Event
    import photo_worker.runtime.transport as module
    stop=Event();stop.set()
    monkeypatch.setattr(module.pika,'BlockingConnection',lambda *a:pytest.fail('connection opened'))
    executor=SimpleNamespace(worker_id=uuid4())
    assert Transport('amqp://localhost','media',executor,development=True).consume(stop)==0


def test_buffered_delivery_after_capacity_stop_never_starts_operation(monkeypatch):
    from threading import Event
    import photo_worker.runtime.transport as module
    stop=Event();events=[]
    class Channel:
        is_open=True
        def basic_qos(self,**kwargs):assert kwargs=={'prefetch_count':1}
        def basic_consume(self,queue,callback,auto_ack):
            assert auto_ack is False
            self.callback=callback
        def basic_ack(self,*args):pytest.fail('stopped delivery acknowledged')
        def basic_reject(self,*args,**kwargs):pytest.fail('stopped delivery rejected')
    channel=Channel()
    class Connection:
        is_open=True
        def channel(self):return channel
        def process_data_events(self,**kwargs):
            stop.set()
            channel.callback(channel,SimpleNamespace(delivery_tag=1),None,b'queued-command')
        def close(self):events.append('close');self.is_open=False
    monkeypatch.setattr(module.pika,'BlockingConnection',lambda *a:Connection())
    executor=SimpleNamespace(worker_id=uuid4(),process=lambda *a:pytest.fail('operation started'),abort_active=lambda:events.append('abort'))
    assert Transport('amqp://localhost','media',executor,development=True).consume(stop)==0
    assert events==['abort','close']


def test_abort_failure_still_closes_connection_and_forbids_reconnect(monkeypatch):
    from threading import Event
    from photo_worker.runtime.transport import WorkerNotStopped
    import photo_worker.runtime.transport as module
    stop=Event();events=[]
    class Channel:
        is_open=True
        def basic_qos(self,**kwargs):pass
        def basic_consume(self,*args,**kwargs):pass
    class Connection:
        is_open=True
        def channel(self):return Channel()
        def process_data_events(self,**kwargs):stop.set()
        def close(self):events.append('close');self.is_open=False
    def abort():
        events.append('abort')
        raise RuntimeError('fixture abort failure')
    monkeypatch.setattr(module.pika,'BlockingConnection',lambda *args:Connection())
    executor=SimpleNamespace(worker_id=uuid4(),abort_active=abort)
    transport=Transport('amqp://localhost','media',executor,development=True)
    with pytest.raises(WorkerNotStopped,match='cancellation failed') as caught:
        transport.run(stop)
    assert isinstance(caught.value.__cause__,RuntimeError)
    assert events==['abort','close']
