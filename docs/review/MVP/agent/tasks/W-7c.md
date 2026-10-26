---
id: W-7c
title: Worker RPC: Go value codec validated against the shared vectors
area: backend
size: S
depends: [W-7b]
edit:
  - backend/internal/workerapi/codec.go (new)
  - backend/internal/workerapi/codec_test.go (new)
context:
  - tests/contracts/worker_rpc_vectors.json
verify:
  - cd backend && go test ./internal/workerapi -run TestCodec -count=1
  - cd backend && go vet ./internal/workerapi
---
## Goal
 Go side of the value codec of the worker RPC. Behaviour is fixed by `tests/contracts/worker_rpc_vectors.json` (created by W-7b); the Go test must load that file.

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

 ## Spec (package `workerapi`)
 - `func DecodeArg(raw json.RawMessage) (any, error)`: returns `nil`, `bool`, `string`, `float64`, `int64`, `uuid.UUID` (github.com/google/uuid), `time.Time` (UTC), `[]byte`, `JSONValue` (a named `type JSONValue json.RawMessage`) or `[]string` for a JSON array of strings. Use `json.Decoder.UseNumber()`; integers that fit int64 become `int64`; numbers with a fraction or exponent become `float64`. Unknown tag -> `ErrUnknownTag`; bad payload -> `ErrBadArgs` (both exported `errors.New` sentinels whose text is the code, e.g. `RPC_BAD_ARGS`).
 - `func EncodeValue(oid uint32, v any) (json.RawMessage, error)`: `oid` is the PostgreSQL type OID of the column. Mapping: uuid (2950) from `[16]byte` or `uuid.UUID` or string; timestamptz (1184) from `time.Time` -> ts tag; bytea (17) -> bytes tag; json (114) / jsonb (3802) -> json tag (value may be `[]byte`, `string` holding JSON, or an already decoded `map[string]any` / `[]any`); text[] (1009) / varchar[] (1015) -> plain array of strings; text (25), varchar (1043), name (19) -> string; bool (16); int2/int4/int8 (21, 23, 20) -> number or int tag when |n| >= 2^53; float4/float8 (700, 701); NULL (`v == nil`) -> `null`. Any other OID -> `ErrUnsupportedType` (`RPC_UNSUPPORTED_TYPE`).
 - Tests: table-driven over the vectors file (decode each `wire` -> compare to the expected Go value; encode with the matching OID -> compare JSON semantically), plus negative cases (naive timestamp string in a `ts` tag, uppercase uuid is accepted and re-encoded lowercase, unknown tag, `numeric` OID 1700 -> unsupported).
 ## Do not
 - No new dependencies beyond `github.com/google/uuid` and the stdlib.
