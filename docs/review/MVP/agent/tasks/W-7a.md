---
id: W-7a
title: Worker RPC: allowlist generator + drift test (SQL surface of TaskDatabase)
area: worker
size: S
depends: []
edit:
  - scripts/gen_worker_rpc_allowlist.py (new)
  - backend/internal/workerapi/rpc_allowlist.json (new)
  - workers/tests/test_worker_rpc_allowlist.py (new)
context:
  - workers/photo_worker/runtime/database.py::TaskDatabase.claim
  - workers/photo_worker/runtime/database.py::TaskDatabase.heartbeat_policy_worker
  - workers/photo_worker/runtime/database.py::TaskDatabase.local_portrait_lease_ready
verify:
  - python3 scripts/gen_worker_rpc_allowlist.py --check
  - cd workers && python -m pytest tests/test_worker_rpc_allowlist.py -q -p no:cacheprovider
---
## Goal
 Plan item W-7 replaces the worker's direct PostgreSQL access by an authenticated HTTP API, WITHOUT rewriting the ~100 thin wrappers in `database.py`: they all go through `TaskDatabase._query(sql, params)` (plus 5 direct `conn.execute` places). The server may only run SQL it already knows, so we need a generated allowlist of every statement the worker can issue.

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
 `scripts/gen_worker_rpc_allowlist.py` (stdlib only, Python 3.9+):
 - Parse `workers/photo_worker/runtime/database.py` with `ast`. Collect every string literal (also implicit concatenations, triple-quoted) that matches `^\s*SELECT\b.*\bphoto\.([a-z0-9_]+)\(` and is used as the FIRST argument of `self._query(...)` or `conn.execute(...)`.
 - For each: `sql` = the literal with whitespace runs collapsed to one space; `arity` = number of `%s` placeholders; `fn` = captured function name; `shape` = `"table"` if the text contains `FROM photo.` (i.e. `SELECT * FROM photo.f(...)` / `SELECT col FROM photo.f(...)`) else `"scalar"`; `mode` = `"all"` if the call chain ends with `.fetchall()` else `"one"`; `worker_args` = indexes of the argument tuple elements whose source text contains `worker_id` (parse the second argument of the call when it is a tuple literal; `[]` when not a literal).
 - `id` = `fn` if unique else `fn.<shape>.<n>` where n is the 1-based index among equal fn in source order.
 - Output JSON (sorted by id, 2-space indent, trailing newline): `{"version":1,"queries":[{"id":...,"sql":"SELECT ... $1,$2 ...","arity":N,"mode":...,"worker_args":[...]}]}`; in `sql` replace the k-th `%s` by `$k`.
 - `--check` regenerates in memory and exits 1 with a unified diff if `backend/internal/workerapi/rpc_allowlist.json` differs; without it the file is (re)written.
 `workers/tests/test_worker_rpc_allowlist.py`: runs the script with `--check` through `subprocess` (expects 0); asserts that the ids `claim_task`, `heartbeat_task`, `commit_task_success`, `commit_task_failure` and `heartbeat_worker_capacity_v2` exist; asserts every `arity` equals the number of `$n` in `sql`.
 Statements that are not a single literal (f-strings, concatenation with variables) must make the script exit 2 naming the line - do not guess.

 ## Do not
 - Do not edit `database.py`. Do not add SQL that is not found in it.
