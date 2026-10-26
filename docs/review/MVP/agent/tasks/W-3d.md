---
id: W-3d
title: Deliver spooled results on worker start
area: worker
size: S
depends: [W-3b, W-3c]
edit:
  - workers/photo_worker/runtime/commit_spool.py
  - workers/photo_worker/runtime/service.py
  - workers/tests/test_commit_spool_replay.py (new)
context:
  - workers/photo_worker/runtime/commit_spool.py::CommitSpool
  - workers/photo_worker/runtime/service.py::_run_built_worker
  - workers/photo_worker/runtime/service.py::WorkerConfig
verify:
  - cd workers && python -m pytest tests/test_commit_spool_replay.py tests/test_commit_spool.py -q -p no:cacheprovider
  - cd workers && python -m pytest tests/test_service_lazy_imports.py -q -p no:cacheprovider
---
## Goal
After a restart, deliver every spooled result BEFORE the worker takes new tasks.

## Spec
1. In `commit_spool.py` add `def replay(spool, database, commit_with_retry, log=None) -> dict`:
   for each `(path, rec)` in `spool.entries()`: `lease = record_to_lease(rec["lease"])`; success -> `database.commit_success(lease, base64-decoded payload)` (first tuple element is the outcome); failure -> rebuild `TaskError.model_validate(rec["failure"])`, `TaskMetrics.model_validate(rec["metrics"])` (import lazily) and `database.commit_failure(...)`. Wrap each call in `commit_with_retry(call)` (a callable that takes a zero-arg function). Outcomes `committed|replayed|stale|cancelled|conflict` -> `spool.remove(path)`; any exception on one entry is logged by class name only and the entry is kept. Return counters `{"delivered": n, "kept": m}`.
2. In `service.py` `_run_built_worker`, after `database.ping()` and before the registration checks, if `transport.executor.spool is not None`: call `replay(transport.executor.spool, database, lambda call: transport.executor._commit_with_retry(None, call))`.
3. Where the executor is built from `WorkerConfig` (search `Executor(` in `service.py`; if it needs more context than shown, answer `BLOCKED: need Executor( construction site`), pass `spool=CommitSpool(config.scratch_root / '.commit-spool')` unless the env var `WORKER_COMMIT_SPOOL=off`.

## Tests
`FakeDB` with `commit_success`/`commit_failure` recording calls and returning scripted outcomes; real `CommitSpool` in `tmp_path` (use the same fake-lease helper idea as in `test_commit_spool.py`, but `record_to_lease` needs a real `Lease`, so monkeypatch `commit_spool.record_to_lease` to return the fake lease). Assert: delivered entries are removed; exception on one entry keeps it and others still proceed; stale outcome removes.
