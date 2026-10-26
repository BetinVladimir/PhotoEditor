---
id: W-7e
title: Worker RPC: pgx adapter (Beginner/Tx) and database-role guard
area: backend
size: S
depends: [W-7d]
edit:
  - backend/internal/workerapi/pgx_adapter.go (new)
  - backend/internal/workerapi/pgx_adapter_test.go (new)
context:
  - backend/internal/gateway/service.go:176-230
verify:
  - cd backend && go build ./... && go vet ./internal/workerapi
  - cd backend && go test ./internal/workerapi -run TestPgxAdapter -count=1
---
## Goal
Real implementation of `Beginner`/`Tx` over `pgxpool`, mirroring how the object gateway opens its pool (role pinning, timeouts).
## Spec
- `func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)`: `pgxpool.ParseConfig`; set `ConnConfig.RuntimeParams["role"] = "photo_worker"`, `statement_timeout=30000`, `lock_timeout=3000`, `idle_in_transaction_session_timeout=30000`; call `database.GuardApplicationRole(config)` exactly as `backend/internal/gateway/service.go::Run` does (read it); `MaxConns` 8.
- `type PoolBeginner struct{ Pool *pgxpool.Pool }` implementing `Beginner`: `Begin` uses `pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})`.
- `Tx.Query`: `tx.Query(ctx, sql, args...)`; read `rows.FieldDescriptions()` OIDs, `rows.Values()` per row, `rows.Err()`; close rows before the next call. Convert `pgtype.UUID`/`[16]byte` values as `EncodeValue` expects.
- Unit test without a database: assert that `NewPool` rejects an unparsable URL and that the role runtime param is set (inspect through a helper `func poolConfig(databaseURL string) (*pgxpool.Config, error)` that `NewPool` uses). A DB integration test is out of scope (skipped here).
## Do not
- Do not add SQL. Do not use `SELECT` strings in this file.
