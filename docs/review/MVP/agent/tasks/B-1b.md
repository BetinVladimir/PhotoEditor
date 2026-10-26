---
id: B-1b
title: Server.limit middleware and portal session limit
area: backend
size: S
depends: [B-1a]
edit:
  - backend/internal/httpapi/ratelimit.go
  - backend/internal/httpapi/server.go
  - backend/internal/httpapi/portal.go
  - backend/internal/httpapi/ratelimit_middleware_test.go (new)
context:
  - backend/internal/httpapi/ratelimit.go
  - backend/internal/httpapi/server.go::Server
  - backend/internal/httpapi/server.go::writeError
  - backend/internal/httpapi/portal.go:12-20
  - backend/internal/httpapi/clientip.go
verify:
  - cd backend && go test ./internal/httpapi -run 'TestLimit|TestLimiter|TestClientIP' -count=1
---
## Goal
Add `s.limit(class)` and apply it to `POST /api/v1/portal/sessions` (brute-force of share secrets).

## Spec
1. `server.go`: add field `Limiter RateLimiter` to `Server` (anywhere in the struct, one line).
2. `ratelimit.go`: add
```go
var defaultLimiter = NewMemoryLimiter()
func (s *Server) limit(class string) func(http.Handler) http.Handler
```
Behaviour: enabled only if `s.Limiter != nil` OR `s.Config.Environment == "production"`; otherwise the middleware returns `next` unchanged (keeps existing tests and development unaffected). The limiter used is `s.Limiter` or `defaultLimiter`. Key: `class + "|" + s.clientIP(r)`. Rule from `DefaultRules[class]` (unknown class -> pass through). On deny: `w.Header().Set("Retry-After", strconv.Itoa(ceilSeconds))` and `writeError(w, r, domain.NewError("RATE_LIMITED", 429))` (check how `domain.NewError` is used in `writeError`'s callers; import `strconv`, `domain` as needed). Use `time.Now()` for `now`.
3. `portal.go`: in `portalRoutes`, register the sessions route as `r.With(s.limit("portal_session")).Post("/api/v1/portal/sessions", ...)` (only change that one registration line; the handler body stays).

## Tests
`httptest` with a `chi.NewRouter()`, `s := &Server{Limiter: NewMemoryLimiter()}`: `r.With(s.limit("portal_session")).Get("/x", ok)`: the 11th request from the same `RemoteAddr` returns 429 with `Retry-After`, a different `RemoteAddr` is still allowed; with `Server{}` in development no limiting; production config without `Limiter` limits.
