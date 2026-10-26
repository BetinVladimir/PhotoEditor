---
id: B-4a
title: Server-side MVP profile guard for deferred API groups
area: backend
size: S
depends: [B-0a]
edit:
  - backend/internal/config/config.go
  - backend/internal/httpapi/profile.go (new)
  - backend/internal/httpapi/server.go
  - backend/internal/httpapi/profile_test.go (new)
context:
  - backend/internal/httpapi/server.go::Server.Routes
  - backend/internal/config/config.go::Load
verify:
  - cd backend && go test ./internal/httpapi -run 'TestMVPProfile' -count=1
  - cd backend && go test ./internal/config -count=1
---
## Goal
The client hides interchange/signing/training screens, but the API must refuse them too (feature stays in the code, disabled by profile).

## Spec
1. `config.go`: field `Profile string` from env `PHOTO_PROFILE`, default `"full"`; allowed values `"full"` and `"mvp"` (else error `PHOTO_PROFILE must be full or mvp`).
2. `profile.go`:
```go
var mvpDeferredFragments = []string{"/interchange-", "/signing-policies", "/training-sets", "/training"}
func (s *Server) mvpProfileGuard(next http.Handler) http.Handler
```
When `s.Config.Profile == "mvp"` and `r.URL.Path` contains any fragment -> respond `404` with `writeError(w, r, domain.NewError("FEATURE_DISABLED", 404))`. Otherwise `next`. (`/training` alone must only match as a path segment: implement fragment matching with `strings.Contains(path, fragment)` for the ones ending with `-`/`s` and a segment check for `/training` - i.e. `/training` followed by end or `/`.)
3. `server.go` `Routes()`: `r.Use(s.mvpProfileGuard)` immediately after the existing `r.Use(s.cors)` line.

## Tests (`TestMVPProfile...`)
Table: with Profile "mvp": `/api/v1/tenants/x/interchange-jobs`, `/api/v1/tenants/x/exports/y/signing-policies`, `/api/v1/tenants/x/training-sets/z`, `/api/v1/tenants/x/styles/s/training-sets` -> 404 FEATURE_DISABLED; `/api/v1/tenants/x/projects`, `/api/v1/tenants/x/trainings-list` -> pass through; with Profile "full" or "" nothing is blocked.
