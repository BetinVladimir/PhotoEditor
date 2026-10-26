---
id: B-2c
title: Private metrics listener (METRICS_LISTEN)
area: backend
size: XS
depends: [B-2a]
edit:
  - backend/cmd/photo/main.go
  - backend/internal/config/config.go
context:
  - backend/cmd/photo/main.go:685-700
  - backend/internal/config/config.go::Load
verify:
  - cd backend && go build ./cmd/photo ./internal/config
  - cd backend && go test ./internal/config -count=1
---
## Spec
1. `config.go`: new field `MetricsListen string` filled from env `METRICS_LISTEN` (default empty = disabled). Validation: if non-empty it must parse with `net.SplitHostPort`, otherwise return an error `METRICS_LISTEN must be host:port`.
2. `main.go`: right after the line `go func() { failure <- server.ListenAndServe() }()` add (do not touch the huge `server := ...` line above it):
```go
if cfg.MetricsListen != "" {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Default.Handler())
	go func() { failure <- (&http.Server{Addr: cfg.MetricsListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe() }()
}
```
and add the import `"photoeditor/backend/internal/metrics"` (and `net/http` if missing - check the existing import block shown by the compiler error rather than guessing; if you cannot see the import block answer `BLOCKED: need import block of main.go`). The `failure` channel has capacity 1: change its creation to `make(chan error, 2)`.
