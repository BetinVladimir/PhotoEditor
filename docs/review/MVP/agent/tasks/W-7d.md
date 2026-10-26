---
id: W-7d
title: Worker RPC: Go handler with allowlist, transaction batching and fake-querier tests
area: backend
size: S
depends: [W-7c, W-7a]
edit:
  - backend/internal/workerapi/handler.go (new)
  - backend/internal/workerapi/handler_test.go (new)
context:
  - backend/internal/workerapi/rpc_allowlist.json
verify:
  - cd backend && go test ./internal/workerapi -count=1
  - cd backend && go vet ./internal/workerapi
---
## Goal
 The HTTP handler of the worker RPC. It knows nothing about pgx: it talks to an interface so it can be tested with a fake.

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
 ```go
 type Row = []any          // values already decoded by DecodeArg's counterpart: raw driver values + the column OIDs
 type Result struct { OIDs []uint32; Rows [][]any }
 type Tx interface {
     Query(ctx context.Context, sql string, args []any) (Result, error)
     Commit(ctx context.Context) error
     Rollback(ctx context.Context) error
 }
 type Beginner interface { Begin(ctx context.Context) (Tx, error) }
 type WorkerIdentity func(r *http.Request) (workerID uuid.UUID, ok bool) // supplied by W-8; the handler only calls it
 func NewHandler(db Beginner, identity WorkerIdentity) http.Handler
 ```
 - `//go:embed rpc_allowlist.json` in a new file `allowlist.go` (add it to this card's edit list); parse once into `map[id]entry`.
 - Method must be POST and path `/worker/v1/rpc`; body limit 1 MiB (`http.MaxBytesReader`); 1..4 calls; `args` length must equal `arity` else 400 `RPC_BAD_ARGS`; unknown `q` -> 400 `RPC_UNKNOWN_QUERY`.
 - Identity: `identity(r)` must return ok, else 401 `{"error":"RPC_UNAUTHENTICATED"}`. For every index `i` in the entry's `worker_args`, the decoded arg must be a `uuid.UUID` equal to the identity; otherwise the WHOLE request is rejected with 403 `RPC_WORKER_MISMATCH` before any database access.
 - Execution: `Begin`, run calls in order, build results with `EncodeValue(oid, v)` per column (mode `one`: first row or `null`; mode `all`: all rows, at most 10000 else 500 `RPC_FAILED`), apply `abort_unless_true` (rollback + `{"aborted":i}`), `Commit`. On any Go error: `Rollback`, map errors implementing `interface{ SQLState() string }` with state `40001` or `40P01` to 503 + `RPC_RETRYABLE`, everything else to 500 `RPC_FAILED` (never include the driver message in the body; log it with `slog`).
 - Tests with a fake `Beginner`: happy path for one call; two calls with `abort_unless_true:[0]` where call 0 returns `false` (second call not executed, rollback called, body `{"aborted":0}`); worker mismatch (no `Begin` call at all); unknown query; wrong arity; retryable error mapping; body too large; response JSON never contains the SQL text.
 ## Do not
 - Never execute SQL taken from the request. Do not log argument values.
