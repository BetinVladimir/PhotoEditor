from __future__ import annotations
from concurrent.futures import Future
from threading import Event, Thread
from time import monotonic
from urllib.parse import urlparse
from uuid import UUID
import logging
import pika
from .executor import Executor, PoisonCommand

QUEUES = {name:'q.worker.'+name for name in ('cpu','gpu','media','archive','collection')}
log = logging.getLogger(__name__)


class WorkerNotStopped(RuntimeError):
    """Restart the process; never start another heavy operation on this node."""


class Transport:
    """AMQP connection/channel always belong to the calling thread.

    Computation runs on one daemon thread; the transport continues pumping AMQP
    heartbeats. Delivery ACK is sent only after Executor reports a durable result
    or an already-owned/obsolete delivery. Prefetch=1 bounds resource occupancy.
    Queue creation is exclusively the deployment bootstrap's responsibility.
    """
    def __init__(self, url: str, resource_class: str, executor: Executor, *, development: bool = False, targeted: bool = True):
        if resource_class not in QUEUES:
            raise ValueError('Invalid worker resource class')
        parsed = urlparse(url)
        if parsed.scheme != 'amqps' and not (development and parsed.scheme == 'amqp' and parsed.hostname in ('127.0.0.1','localhost','::1')):
            raise ValueError('AMQP requires TLS or a development loopback endpoint')
        if not parsed.hostname:
            raise ValueError('Missing AMQP host')
        if targeted and (not isinstance(executor.worker_id,UUID) or executor.worker_id.int==0):
            raise ValueError('Worker identity required for targeted queue')
        queue = f'q.worker.{executor.worker_id}.{resource_class}' if targeted else QUEUES[resource_class]
        self._url, self.queue, self.executor = url, queue, executor

    def run(self, stop: Event):
        backoff = 1
        while not stop.is_set():
            try:
                self.consume(stop)
                backoff = 1
            except WorkerNotStopped:
                raise
            except Exception:
                log.warning('Worker transport disconnected; unacknowledged task remains recoverable')
                if stop.wait(backoff):
                    return
                backoff = min(30,backoff*2)

    def consume(self, stop: Event, *, max_messages: int | None = None) -> int:
        if max_messages is not None and max_messages < 1:
            raise ValueError('Invalid delivery limit')
        parameters = pika.URLParameters(self._url)
        parameters.heartbeat = 30
        parameters.socket_timeout = 5
        parameters.stack_timeout = 10
        parameters.blocked_connection_timeout = 10
        parameters.connection_attempts = 1
        connection = pika.BlockingConnection(parameters)
        active: Future | None = None
        delivery_tag = None
        processed = 0
        try:
            channel = connection.channel()
            channel.basic_qos(prefetch_count=1)

            def delivered(ch, method, properties, body):
                nonlocal active, delivery_tag
                if active is not None:
                    raise RuntimeError('Worker prefetch invariant violated')
                delivery_tag = method.delivery_tag
                active = Future()
                future = active
                def execute():
                    try:
                        future.set_result(self.executor.process(body))
                    except BaseException as exc:
                        future.set_exception(exc)
                Thread(target=execute,name='photo-operation',daemon=True).start()

            channel.basic_consume(self.queue,delivered,auto_ack=False)
            while not stop.is_set():
                connection.process_data_events(time_limit=.1)
                if active is not None and active.done():
                    try:
                        active.result()
                    except PoisonCommand:
                        channel.basic_reject(delivery_tag,requeue=False)
                        log.warning('Worker rejected invalid task command')
                    else:
                        channel.basic_ack(delivery_tag)
                    active = None
                    processed += 1
                    if max_messages is not None and processed >= max_messages:
                        return processed
                if not channel.is_open or not connection.is_open:
                    raise RuntimeError('Worker channel closed')
            return processed
        finally:
            self.executor.abort_active()
            # Stop fetching before waiting for an in-flight operation to observe
            # its lost lease. Closing deliberately leaves its delivery unacked.
            if connection.is_open:
                try:
                    connection.close()
                except Exception:
                    pass
            if active is not None and not active.done():
                deadline = monotonic()+15
                while not active.done() and monotonic()<deadline:
                    Event().wait(.05)
                if not active.done():
                    raise WorkerNotStopped('Operation did not stop within shutdown grace')
