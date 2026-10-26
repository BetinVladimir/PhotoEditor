---
id: W-3a
title: Tests for Executor._commit_with_retry
area: worker
size: XS
depends: []
edit:
  - workers/tests/test_commit_retry.py (new)
context:
  - workers/photo_worker/runtime/executor.py::Executor._commit_with_retry
verify:
  - cd workers && python -m pytest tests/test_commit_retry.py -q -p no:cacheprovider
---
## Goal
Lock down the retry semantics of the terminal commit.

## Spec
`Executor._commit_with_retry(stop, call, attempts=COMMIT_RETRY_ATTEMPTS)` is a static method. Monkeypatch `photo_worker.runtime.executor.sleep` to a recorder so the tests never sleep. Tests:
1. `test_returns_value_without_retry`: call returns `('committed', 1, 2)` -> returned, no sleeps.
2. `test_retries_transport_errors_then_succeeds`: raises `OSError` twice, then returns; result returned; sleeps recorded == [1.0, 2.0].
3. `test_gives_up_after_last_attempt`: always raises `TimeoutError`; with `attempts=3` the third error propagates and the call count is 3.
4. `test_non_transport_error_is_not_retried`: raises `ValueError` -> propagates after exactly one call.
5. `test_delay_is_capped`: `attempts=8`, always `OSError`; the largest recorded sleep is 15.0.
Also a test with `psycopg.OperationalError('x')` raised once (import `psycopg`).
