---
id: W-7h
title: Worker RPC: Go "worker-api" role (mTLS listener) wired into the process entry point
area: backend
size: S
depends: [W-7e, W-8a]
edit:
  - backend/cmd/photo/worker_api.go (new)
  - backend/cmd/photo/worker_api_test.go (new)
  - backend/cmd/photo/main.go
context:
  - backend/cmd/photo/main.go:100-175
  - backend/internal/gateway/service.go:150-178
verify:
  - cd backend && go build ./... && go vet ./cmd/photo
  - cd backend && go test ./cmd/photo -run TestWorkerAPI -count=1
---
## Goal
Start the worker control plane as a process role, next to `object-gateway`.
## Spec
- Study how `cfg.Role == "object-gateway"` is dispatched in `main.go` and how `gateway.LoadServerTLS` builds the mTLS config. Add `cfg.Role == "worker-api"` the same way.
- `worker_api.go`: `func runWorkerAPI(ctx context.Context, cfg ...) error` (use the same config struct the gateway branch uses; new env: `WORKER_API_ADDR` default `:8444`, reuse the gateway TLS file settings if they exist - read `LoadServerTLS` for the field names; if a separate file set is needed, add fields named `WorkerAPICertificate` / `WorkerAPIPrivateKey` / `WorkerAPIClientCA` and document them in a comment). Build `workerapi.NewPool(...)`, `workerapi.NewHandler(workerapi.PoolBeginner{Pool: pool}, identity)` where `identity` comes from W-8a (`workerapi.IdentityFromTLS`), serve with `http.Server{ReadHeaderTimeout: 5s, ReadTimeout: 30s, WriteTimeout: 60s}` over `tls.NewListener`, graceful shutdown on `ctx.Done()`, `/health/live` and `/health/ready` (ready = `pool.Ping`).
- Test: `TestWorkerAPIHandlerRequiresClientCert` starts the TLS server on 127.0.0.1:0 using in-memory certificates generated in the test (copy the helper approach from `backend/internal/gateway/wire_integration_test.go`) with a fake `Beginner`; a client without a certificate must fail the handshake; with a valid worker certificate `POST /worker/v1/rpc` of an unknown query returns 400 `RPC_UNKNOWN_QUERY`.
## Do not
- Do not expose the listener in compose.yaml here (card W-7i). Do not log request bodies.
