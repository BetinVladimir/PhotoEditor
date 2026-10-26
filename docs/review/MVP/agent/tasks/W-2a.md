---
id: W-2a
title: Transport.paused - stop taking new tasks without dropping the active one
area: worker
size: S
depends: []
edit:
  - workers/photo_worker/runtime/transport.py
  - workers/tests/test_transport_pause.py (new)
context:
  - workers/photo_worker/runtime/transport.py::Transport.__init__
  - workers/photo_worker/runtime/transport.py::Transport.consume
verify:
  - cd workers && python -m pytest tests/test_transport_pause.py -q -p no:cacheprovider
---
## Goal
When the capacity heartbeat is degraded (next cards), the worker must stop *starting* new tasks but must not abort the one that is running (its lease decides its fate).

## Spec
In `Transport.__init__` add `self.paused = Event()` (already imported).
In `Transport.consume`:
- remember the tag: `consumer_tag = channel.basic_consume(self.queue, delivered, auto_ack=False)` (replaces the bare call);
- inside the `while not stop.is_set():` loop, before `process_data_events`:
  - `if self.paused.is_set() and consumer_tag is not None:` -> `pending = channel.basic_cancel(consumer_tag)`; `consumer_tag = None`; for every `(method, props, body)` in `pending or []` call `channel.basic_nack(method.delivery_tag, requeue=True)`;
  - `elif not self.paused.is_set() and consumer_tag is None:` -> `consumer_tag = channel.basic_consume(self.queue, delivered, auto_ack=False)`.
- An already delivered (unacked) message keeps being processed and acked as before.
- Do not change reconnect/backoff logic or timeouts in this card.

## Test (`workers/tests/test_transport_pause.py`)
Monkeypatch `pika.URLParameters` and `pika.BlockingConnection` (module `photo_worker.runtime.transport`) with fakes: `FakeConnection` (`is_open=True`, `channel()`, `process_data_events(time_limit)`, `close()`), `FakeChannel` (`is_open=True`, `basic_qos`, `basic_consume` returning `'tag-N'` and recording calls, `basic_cancel` returning `[]` and recording, `basic_ack`, `basic_reject`, `basic_nack`). Build `Transport('amqps://u:p@h/v', 'cpu', executor, development=False, targeted=False)` with `executor = SimpleNamespace(worker_id=None, abort_active=lambda: None, process=lambda body: 'ok')`.
`process_data_events` of the fake connection runs a scripted list of callables (first sets `transport.paused`, next clears it, last sets `stop`). Assert: after pause exactly one `basic_cancel('tag-1')`; after unpause a second `basic_consume`; the loop exits cleanly.
