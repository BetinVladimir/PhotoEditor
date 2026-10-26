---
id: W-3c
title: Spool the result before commit in Executor.process
area: worker
size: S
depends: [W-3b]
edit:
  - workers/photo_worker/runtime/executor.py
  - workers/tests/test_executor_spool.py (new)
context:
  - workers/photo_worker/runtime/executor.py::Executor.__init__
  - workers/photo_worker/runtime/executor.py::Executor._commit_with_retry
  - workers/photo_worker/runtime/commit_spool.py::CommitSpool
verify:
  - cd workers && python -m pytest tests/test_executor_spool.py tests/test_executor_installation_boundary.py tests/test_commit_retry.py -q -p no:cacheprovider
---
## Goal
Make the terminal commit survive restarts: spool first, remove only after SQL's verdict. Because `Executor.process` is long, put the new logic in two small helper methods and keep the call sites tiny.

## Spec
1. `Executor.__init__`: new keyword-only argument `spool=None` (a `CommitSpool` or None); store as `self.spool`.
2. New method `def _commit(self, ctx, kind, *, payload=None, failure=None, metrics=None)` that:
   - if `self.spool` is set: `path = self.spool.write(ctx.lease, kind, payload=payload, failure=failure, metrics=metrics)` (a failure to write must NOT prevent the commit: catch `OSError`, `path=None`);
   - performs the commit through `self._commit_with_retry(...)`: for `'success'` -> `self.database.commit_success(ctx.lease, payload)` (returns a tuple; return its first element), for `'failure'` -> `self.database.commit_failure(ctx.lease, failure, metrics)`;
   - if the outcome is in `('committed','replayed','stale')` and `path` is not None -> `self.spool.remove(path)`;
   - returns the outcome string. Exceptions propagate and leave the spool file in place.
3. In `Executor.process`, in the block that currently calls `_commit_with_retry` directly three times, replace those calls by `self._commit(...)`. Keep the `cancelled` special case: when the success commit returns `'cancelled'`, build the `TaskCancelled` `TaskError` as today and call `self._commit(ctx, 'failure', failure=error, metrics=metrics)`.
4. Do not change any other behaviour.

## Test (`test_executor_spool.py`)
Do not run the whole `process`; test `_commit` directly: create `Executor(object(), uuid4(), [], spool=FakeSpool())`; set `executor.database = FakeDB`. Cases: (a) success -> spool written then removed; (b) DB raises `ValueError` -> spool file remains, exception propagates; (c) outcome `'conflict'` -> spool remains and `'conflict'` is returned; (d) spool.write raises `OSError` -> commit still happens; (e) no spool configured -> works. Use `unittest.mock.patch('photo_worker.runtime.executor.sleep')` if retries could sleep.
