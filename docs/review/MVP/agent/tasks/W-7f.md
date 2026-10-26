---
id: W-7f
title: Worker RPC: Python HTTP client implementing the TaskDatabase query seam
area: worker
size: S
depends: [W-7b]
edit:
  - workers/photo_worker/runtime/rpc_database.py (new)
  - workers/tests/test_rpc_database.py (new)
context:
  - workers/photo_worker/runtime/database.py::TaskDatabase._connect
  - workers/photo_worker/runtime/database.py::TaskDatabase._query
  - workers/photo_worker/runtime/database.py::TaskDatabase.accelerator_operations
  - workers/photo_worker/runtime/gateway.py:1-60
verify:
  - cd workers && python -m pytest tests/test_rpc_database.py -q -p no:cacheprovider
---
## Goal
 `RpcTaskDatabase(TaskDatabase)` overrides ONLY the query seam, so all ~100 wrappers keep working unchanged over HTTPS.

### Wire format "worker RPC v1" (single source of truth for cards W-7*)
Request `POST /worker/v1/rpc`, `Content-Type: application/json`:
`{"calls":[{"q":"<allowlist id>","args":[<value>...],"mode":"one"|"all"}], "abort_unless_true":[<call index>...]}`
- `calls` has 1..4 items and runs in ONE database transaction, in order.
- If `abort_unless_true` lists index `i` and the first column of the first row of call `i` is not exactly `true`, the transaction is rolled back, later calls are not executed and the response is `{"aborted":i}` (HTTP 200).
- `q` is an id from `backend/internal/workerapi/rpc_allowlist.json`; the server executes ONLY the SQL stored there, never SQL sent by the client.
Response 200: `{"results":[{"row":[<value>...]|null}|{"rows":[[<value>...],...]}]}` (`row` for mode `one`, `rows` for `all`).
Errors: HTTP 4xx/5xx with `{"error":"<CODE>"}`; codes `RPC_UNKNOWN_QUERY`, `RPC_BAD_ARGS`, `RPC_WORKER_MISMATCH`, `RPC_UNSUPPORTED_TYPE`, `RPC_RETRYABLE` (serialization failure / deadlock: the client retries like `TaskDatabase._query` does), `RPC_FAILED`.
Values: JSON null/bool/string/float are themselves; integers are JSON numbers when |n| < 2^53 else `{"$":"int","v":"<decimal>"}`; every other type is tagged:
`{"$":"uuid","v":"<lowercase canonical>"}`, `{"$":"ts","v":"<RFC3339 UTC with Z, 6 fractional digits>"}` (timezone-aware instants only), `{"$":"bytes","v":"<base64 std>"}`, `{"$":"json","v":<any JSON>}` (jsonb/json values and `Jsonb(...)` arguments), lists of strings (text[]) are plain JSON arrays of strings.
Anything else (numeric, interval, date, ...) is an error `RPC_UNSUPPORTED_TYPE`; do not invent encodings.

 ## Spec
 - `class RpcTaskDatabase(TaskDatabase)`; `__init__(self, base_url, tls_context, *, timeout=20.0, allowlist_path=None)`; it does NOT call `super().__init__` (no database URL). Load `backend/internal/workerapi/rpc_allowlist.json` (path argument; default: package data copy `photo_worker/runtime/rpc_allowlist.json` - add a test that both files are byte-identical when the second exists) into `{normalized sql with %s: id}` (convert `$k` back to `%s`).
 - `_query(self, query, params)`: normalise whitespace of `query`; look up the id (unknown -> `ValueError('RPC_UNKNOWN_QUERY')`); POST one call (`mode` from the allowlist entry) with `rpc_codec.encode_arg` per parameter; decode the response with `decode_row` (return `None` for `row: null`, else a `tuple`). Retry up to 3 times with `sleep(.01*(attempt+1))` on HTTP 503 with `RPC_RETRYABLE` exactly like `TaskDatabase._query`; map other HTTP errors to `ConnectionError(code)` (the existing code treats `ConnectionError`/`OSError` as transient).
 - Use `http.client.HTTPSConnection` with the given `ssl.SSLContext` (see `gateway.py` for the style); one short-lived connection per call is acceptable.
 - Override `heartbeat_policy_worker_with_residency` to send ONE request with two calls (`heartbeat_worker_capacity_v2`, `report_worker_model_residency`) and `abort_unless_true:[0,1]`; return `False` if `aborted` is present, else `True`. Keep the argument validation of the original (copy the `if` block).
 - Override `accelerator_operations`, `runtime_operations` (they use `conn.execute(...).fetchall()`): single call with mode `all`, return `frozenset(row[0] for row in rows)` exactly as the original does (read it).
 - Do NOT override `task_result_fence_ready`, `_worker_function_set_ready`, `ping`, `_connect`, `_require_role`; instead make them raise `NotImplementedError('RPC_NOT_SUPPORTED')` here so that the card W-7g decides how startup checks run in RPC mode.
 - Tests with an in-process fake HTTP server (`http.server` on 127.0.0.1, plain HTTP is allowed in tests via a `scheme` argument) that checks the request JSON and returns canned responses: claim round trip (rows with uuid, ts, json tags -> `Claim`/`Lease` objects equal to what the psycopg path builds), `heartbeat` returns `Heartbeat`, retry on 503, unknown query, residency batch.
 ## Do not
 - Do not edit `database.py`. Do not catch exceptions broadly.
