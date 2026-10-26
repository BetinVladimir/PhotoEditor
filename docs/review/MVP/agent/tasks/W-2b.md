---
id: W-2b
title: CapacityReporter degraded/recovered callbacks
area: worker
size: S
depends: []
edit:
  - workers/photo_worker/runtime/capacity.py
  - workers/tests/test_capacity_reporter_tolerance.py
context:
  - workers/photo_worker/runtime/capacity.py::CapacityReporter
  - workers/tests/test_capacity_reporter_tolerance.py
verify:
  - cd workers && python -m pytest tests/test_capacity_reporter_tolerance.py -q -p no:cacheprovider
---
## Goal
After `degraded_after` consecutive transient report failures the reporter calls `on_degraded()` once (the worker then stops taking new tasks); on the next success it calls `on_recovered()`. The hard abort after `grace` seconds and the immediate abort on `RegistrationRejected` stay as they are.

## Spec
- `CapacityReporter.__init__`: new keyword-only arguments `degraded_after: int = 3`, `on_degraded=None`, `on_recovered=None`; store as attributes of the same names.
- In `run`: keep a counter `consecutive` and a flag `degraded`. In the tolerated-failure branch (before `continue`): `consecutive += 1`; when `consecutive >= self.degraded_after and not degraded` set `degraded = True` and call `self.on_degraded()` if it is not None, wrapped in `try/except Exception: pass`. On success: `consecutive = 0`; if `degraded`: `degraded = False` and call `on_recovered()` the same safe way.
- Do not change behaviour when the callbacks are None.

## Tests
Add to the existing test file (keep the three existing tests unchanged):
1. `test_degraded_then_recovered_callbacks`: report raises `OSError` 4 times then succeeds then the stop event is set; `degraded_after=2`; assert events == ['degraded', 'recovered'] (degraded exactly once).
2. `test_callbacks_not_called_for_single_blip`: one failure then success with `degraded_after=3` -> no callbacks.
