---
id: B-1c
title: Apply limit classes to portal download/upload routes
area: backend
size: XS
depends: [B-1b]
edit:
  - backend/internal/httpapi/portal.go
context:
  - backend/internal/httpapi/portal.go:193-250
verify:
  - cd backend && go build ./internal/httpapi
  - cd backend && go test ./internal/httpapi -run 'TestLimit' -count=1
---
## Spec
In `portal.go` change exactly these registrations from `r.Post(` to `r.With(s.limit("<class>")).Post(` (nothing else):
- `/api/v1/portal/download-jobs/{id}/download-intent` -> `portal_download`
- `/api/v1/portal/items/{id}/download-intent` -> `portal_download`
- `/api/v1/downloads/redeem` -> `portal_download`
- `/api/v1/portal/uploads` -> `portal_upload`
Do not touch the handler bodies. If other `POST /api/v1/portal/uploads/...` registrations exist in the shown range, apply `portal_upload` to them too.
