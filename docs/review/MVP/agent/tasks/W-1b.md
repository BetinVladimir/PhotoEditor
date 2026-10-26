---
id: W-1b
title: Unit tests for heartbeat tolerance (run_heartbeat_loop)
area: worker
size: S
depends: [W-1a]
edit:
  - workers/tests/test_heartbeat_tolerance.py (new)
context:
  - workers/photo_worker/runtime/heartbeat.py
  - workers/photo_worker/runtime/executor.py::ExecutionContext
verify:
  - cd workers && python -m pytest tests/test_heartbeat_tolerance.py -q -p no:cacheprovider
---
## Goal
Prove the VPN-blip tolerance of the heartbeat loop with fakes only (no DB, no sleeping over 0.5 s in total).

## Spec
Write `workers/tests/test_heartbeat_tolerance.py`. Use fakes:
- `FakeCtx`: attributes `lease=object()`, `_lease_until` (float, monotonic time), `_lost=threading.Event()`, methods `snapshot_progress()->{}` and `accept_heartbeat(beat)->beat.accepted`.
- `FakeDB.heartbeat(lease, progress)`: raises `OSError('vpn')` for the first `n` calls, then returns `SimpleNamespace(accepted=True)`; counts calls.
- Always pass tiny timings: `interval=0.01, backoff_base=0.01, backoff_step=0.0, backoff_cap=0.02`, and run the loop on the test thread with a `stop` Event that the fake DB sets after a given number of successful beats.

Tests (names are mandatory):
1. `test_transient_errors_do_not_mark_lease_lost`: 3 failures then success while `_lease_until = monotonic()+30`; after the loop stops `ctx._lost` is not set and `on_error` was called 3 times.
2. `test_lease_expiry_marks_lost`: DB always raises, `_lease_until = monotonic()+0.05`, `clock` default; loop returns by itself and `ctx._lost.is_set()`.
3. `test_server_refusal_returns_immediately`: DB returns `accepted=False`; loop returns after exactly one call and `_lost` is NOT set by the loop (the context decides that in `accept_heartbeat`; the fake just returns False).
4. `test_stop_during_backoff_returns_without_lost`: DB raises; `stop` is set from `on_error`; loop returns, `_lost` not set.
5. `test_failure_counter_resets_after_success`: failures, success, failures; inject a `clock` that returns a constant far below `_lease_until`; assert total DB calls and that `_lost` is not set.

Keep each test under 25 lines.
