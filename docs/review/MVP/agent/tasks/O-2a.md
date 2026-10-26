---
id: O-2a
title: /health/version endpoint (build identity)
area: backend
size: XS
depends: []
edit:
  - backend/internal/httpapi/version.go (new)
  - backend/internal/httpapi/server.go
  - backend/internal/httpapi/version_test.go (new)
context:
  - backend/internal/httpapi/server.go:112-126
verify:
  - cd backend && go test ./internal/httpapi -run 'TestHealthVersion' -count=1
---
## Goal
Smoke tests must be able to compare "what is deployed" with the release manifest.

## Spec
`version.go`:
```go
var buildRelease = "" // may be set with -ldflags "-X photoeditor/backend/internal/httpapi.buildRelease=..."
func buildIdentity() map[string]any
```
Returns `{"release": buildRelease or env PHOTO_RELEASE_ID, "vcs_revision": <debug.ReadBuildInfo setting vcs.revision or "">, "vcs_modified": <"vcs.modified" == "true">, "go_version": runtime.Version(), "profile": <Config.Profile or "full">}` (make it a method on `*Server` so the profile is available). In `Routes()` register right after `/health/live`: `r.Get("/health/version", func(w, r) { writeJSON(w, 200, s.buildIdentity()) })`. No secrets, no hostnames.

## Test
Request `/health/version` on `(&Server{}).Routes()`; status 200, JSON has the five keys; with `PHOTO_RELEASE_ID` set via `t.Setenv` it appears.
