---
id: W-4b
title: TaskDatabase._query - retry connection failures only
area: worker
size: XS
depends: [W-4a]
edit:
  - workers/photo_worker/runtime/database.py
  - workers/tests/test_database_query_retry.py (new)
context:
  - workers/photo_worker/runtime/database.py::TaskDatabase._query
verify:
  - cd workers && python -m pytest tests/test_database_query_retry.py tests/test_heartbeat_chain_source_pins.py -q -p no:cacheprovider
---
## Goal
A refused/unreachable connection happens BEFORE any statement is sent, so retrying it is always safe (even for non-idempotent calls). Errors during statement execution must keep today's behaviour (no retry, except serialization/deadlock).

## Spec
Rewrite only `_query` (keep the signature `_query(self, query, params)` - tests replace it):
```python
for attempt in range(3):
    try:
        conn = self._connect()      # connection failures are retried here
    except psycopg.OperationalError:
        if attempt == 2: raise
        sleep(0.2 * (attempt + 1)); continue
    try:
        with conn:
            return conn.execute(query, params).fetchone()
    except (psycopg.errors.SerializationFailure, psycopg.errors.DeadlockDetected):
        if attempt == 2: raise
        sleep(.01 * (attempt + 1))
```
(`with conn:` commits/rolls back like the old `with self._connect() as conn`; closing must still happen - check how the old code did it and keep it equivalent. If psycopg3 `with conn` does not close in the shown version, call `conn.close()` in a `finally`.)
`sleep` is already imported in this module.

## Test
Patch `TaskDatabase._connect`: (a) raises `psycopg.OperationalError` twice then returns a fake connection whose `execute(...).fetchone()` returns `(1,)` -> result `(1,)`, 3 connect calls; (b) always OperationalError -> raised after 3 attempts; (c) fake connection whose execute raises `psycopg.OperationalError` -> raised immediately, ONE connect call; (d) SerializationFailure twice then OK. Patch `sleep` in the module.
