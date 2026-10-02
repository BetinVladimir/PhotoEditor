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
