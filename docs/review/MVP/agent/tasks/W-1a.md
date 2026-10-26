---
id: W-1a
title: Extract the task heartbeat loop into runtime/heartbeat.py (no behaviour change)
area: worker
size: S
depends: []
edit:
  - workers/photo_worker/runtime/heartbeat.py (new)
  - workers/photo_worker/runtime/executor.py
context:
  - workers/photo_worker/runtime/executor.py::Executor.process
  - workers/photo_worker/runtime/executor.py::ExecutionContext
verify:
  - cd workers && python -m pytest tests/test_executor_installation_boundary.py tests/test_heartbeat_chain_source_pins.py -q -p no:cacheprovider
---
## Goal
`Executor.process` contains an inner function `heartbeat()` (transient-error tolerant, see comments there). Move the loop into a plain, unit-testable function in a new module and call it from `process`. Behaviour must stay identical.

## Spec
Create `workers/photo_worker/runtime/heartbeat.py` with exactly this public API:

```python
from time import monotonic
from threading import Event

HEARTBEAT_SAFETY_MARGIN_SECONDS = 2.0

def run_heartbeat_loop(ctx, database, stop: Event, interval: float, sample_memory, on_error=None, *,
                       clock=monotonic, backoff_base=1.0, backoff_step=0.5, backoff_cap=5.0) -> None:
```
Semantics (copy them from the current inner function, do not invent new ones):
1. `failures = 0`; loop `while not stop.wait(interval if failures == 0 else 0)`.
2. In `try`: `sample_memory()`; `beat = database.heartbeat(ctx.lease, ctx.snapshot_progress())`; `if not ctx.accept_heartbeat(beat): return`.
3. `except Exception as exc`: call `on_error(exc)` if given; if `clock() >= ctx._lease_until - HEARTBEAT_SAFETY_MARGIN_SECONDS`: `ctx._lost.set(); return`; otherwise wait `min(backoff_cap, backoff_base + backoff_step*failures, interval)` on `stop` (return if it was set), `failures += 1`, `continue`.
4. After a successful beat set `failures = 0`.

In `executor.py`:
- import `HEARTBEAT_SAFETY_MARGIN_SECONDS` and `run_heartbeat_loop` from `.heartbeat` and delete the local constant definition `HEARTBEAT_SAFETY_MARGIN_SECONDS = 2.0` (keep `COMMIT_RETRY_ATTEMPTS`);
- inside `process`, replace the inner `heartbeat()` function by a small closure `sample_memory()` that does `nonlocal peak_memory; peak_memory = max(peak_memory, process.memory_info().rss)` and an `on_error` that calls `emit_failure_metadata(exc,'LEASE_LOST','heartbeat')`;
- `thread = Thread(target=run_heartbeat_loop, args=(ctx, self.database, stop, self.heartbeat_seconds, sample_memory, on_error), name='task-heartbeat', daemon=True)`.

## Do not
- Do not change anything else in `process`, and do not touch `database.py`.
