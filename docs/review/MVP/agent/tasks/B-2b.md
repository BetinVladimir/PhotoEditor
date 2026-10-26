---
id: B-2b
title: HTTP request metrics middleware
area: backend
size: S
depends: [B-2a]
edit:
  - backend/internal/httpapi/metrics.go (new)
  - backend/internal/httpapi/server.go
  - backend/internal/httpapi/metrics_test.go (new)
context:
  - backend/internal/httpapi/server.go::Server.Routes
  - backend/internal/metrics/metrics.go
verify:
  - cd backend && go test ./internal/httpapi -run 'TestRequestMetrics' -count=1
---
## Spec
1. `metrics.go` in package `httpapi`:
```go
var (
	httpRequests = metrics.Default.Counter("photo_http_requests_total", "HTTP requests", "route", "status")
	httpDuration = metrics.Default.Histogram("photo_http_request_duration_seconds", "HTTP latency", []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10}, "route")
)
func requestMetrics(next http.Handler) http.Handler
```
The middleware wraps the `ResponseWriter` to record the status (default 200; preserve `http.Flusher` if the wrapped writer has it) and after `next` uses `chi.RouteContext(r.Context()).RoutePattern()` as the `route` label (fallback `"unmatched"`; NEVER use the raw path - cardinality). Status label = three-digit code string.
2. `server.go` `Routes()`: add `r.Use(requestMetrics)` right after `r.Use(middleware.RequestID, middleware.Recoverer)`.
3. Do NOT register a `/metrics` route on the public router (it is served on a private listener by the next card).

## Tests
Router with `r.Use(requestMetrics)` and `r.Get("/items/{id}", ...)`; call `/items/1` and `/items/2`; scrape `metrics.Default.Handler()` with httptest and assert a single series `route="/items/{id}"` with count 2 and the status `"200"`; a 404 request is labelled `"unmatched"`.
