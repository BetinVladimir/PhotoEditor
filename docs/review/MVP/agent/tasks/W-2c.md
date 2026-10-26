---
id: W-2c
title: Wire reporter degradation to Transport.paused in _run_built_worker
area: worker
size: XS
depends: [W-2a, W-2b]
edit:
  - workers/photo_worker/runtime/service.py
context:
  - workers/photo_worker/runtime/service.py::_run_built_worker
verify:
  - cd workers && python -m pytest tests/test_service_lazy_imports.py -q -p no:cacheprovider
  - cd workers && python -m pytest tests -q -p no:cacheprovider -k "service or capacity or metal" -x
---
## Goal
Connect the pieces: a degraded capacity heartbeat pauses new work, recovery resumes it.

## Spec
In `_run_built_worker`, right after the `reporter = CapacityReporter(...)` assignment (both branches create it) add:
```python
paused = getattr(transport, 'paused', None)
if paused is not None:
    reporter.on_degraded = paused.set
    reporter.on_recovered = paused.clear
```
No other change. Keep the style (the function uses compact code without spaces after commas).
