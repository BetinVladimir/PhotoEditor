---
id: W-7b
title: Worker RPC: Python value codec + shared test vectors
area: worker
size: S
depends: [W-7a]
edit:
  - workers/photo_worker/runtime/rpc_codec.py (new)
  - workers/tests/test_rpc_codec.py (new)
  - tests/contracts/worker_rpc_vectors.json (new)
context: []
verify:
  - cd workers && python -m pytest tests/test_rpc_codec.py -q -p no:cacheprovider
---
## Goal
 Encode query arguments and decode result rows so that the HTTP path returns exactly the Python types psycopg returns today (UUID, aware datetime, bytes, dict/list for jsonb). The same vectors file is consumed by the Go codec card (W-7c) so both sides provably agree.

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

 ## Spec (`rpc_codec.py`)
 - `encode_arg(value)`: `None/bool/str/float` as is; `int` as in the wire format; `uuid.UUID` -> `{"$":"uuid"}`; aware `datetime` -> `{"$":"ts"}` (convert to UTC; naive datetime -> `ValueError('RPC_NAIVE_DATETIME')`); `bytes` -> bytes tag; `psycopg.types.json.Jsonb` (import lazily; use `value.obj`) -> json tag; `dict` -> json tag; `list` of `str` -> plain list; `tuple`/`frozenset`/`set` -> `TypeError`.
 - `decode_value(node)`: inverse; tags `uuid`->`UUID`, `ts`->aware `datetime` (UTC), `bytes`->`bytes`, `json`->the JSON value, `int`->`int`; plain list -> `list`; unknown tag -> `ValueError('RPC_UNKNOWN_TAG')`.
 - `decode_row(nodes)` -> `tuple`.
 - `encode_request(calls, abort_unless_true=())` -> dict as in the wire format (`calls` items are `(q, args, mode)`).
 ## Vectors (`tests/contracts/worker_rpc_vectors.json`)
 `{"version":1,"values":[{"name":..,"python":"<repr hint>","wire":<json>}]}` with at least: null, true, 0, -1, 2^53-1, 2^53 (int tag), 1.5, "" , "ü", uuid, ts with microseconds, ts at epoch, bytes empty, bytes 0..255, json object with nested null/array, json array, json scalar string, text list ["a","b"], empty list. The Python test loads the file, decodes every `wire` and re-encodes the result, expecting an identical JSON document; `python` is documentation only.
 ## Do not
 - Do not use `eval`/`pickle`. No third-party dependencies (psycopg only for the optional Jsonb import).
