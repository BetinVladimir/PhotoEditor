---
id: B-5m-V2Draft
title: Move catalogRoutes block "v2 layers/documents/drafts" into catalogV2DraftRoutes
area: backend
size: XS
depends: [B-5a]
edit:
  - backend/internal/httpapi/catalog.go
  - backend/internal/httpapi/catalog_v2draft.go (new)
context:
  - backend/internal/httpapi/catalog.go:103-170
verify:
  - cd backend && go build ./... && go vet ./internal/httpapi
  - cd backend && go test ./internal/httpapi -run TestRoutesGolden -count=1
  - test -f backend/internal/httpapi/catalog_v2draft.go
---
## Goal
Move the block of `catalogRoutes` that registers: v2 layers/documents/drafts, shot lists into its own method `catalogV2DraftRoutes` in `backend/internal/httpapi/catalog_v2draft.go`. This is a pure cut-and-paste refactor; behaviour must not change. A deterministic script does the edit; you only run it and fix the build if needed. DO NOT retype or rewrite route handlers by hand.

## Steps
1. Dry run (writes nothing):
```
python3 docs/review/MVP/agent/tools/move_routes.py --name V2Draft \
    --first 'r.Get("/api/v1/tenants/{t}/assets/{id}/v2-layers/{artifact}"' \
    --last  'r.Get("/api/v1/tenants/{t}/assets/{id}/v2-draft/history"' \
    --dry-run
```
It prints the line range, the inferred parameters and the call. If it prints `REFUSED: ...`, stop and answer `BLOCKED: <the message>`.
2. Run the same command without `--dry-run`.
3. `cd backend && go build ./... && go vet ./internal/httpapi`. Typical fixes (and ONLY these):
   - `imported and not used` / `undefined: pkg` in `catalog.go` or `catalog_v2draft.go`: adjust the import lists (copy the import line from the other file).
   - `copylocks` from vet: change the parameter type in the new function to a pointer (`*pkg.Type`) and pass `&name` in the call.
4. `cd backend && go test ./internal/httpapi -run TestRoutesGolden -count=1` must pass (the route table is unchanged).

## Do not
- Do not edit route paths, handlers, middleware or the order of the calls in `catalogRoutes`.
- Do not run `gofmt -w` on `catalog.go` (it would reformat unrelated code).
- Do not create more than the one new file `catalog_v2draft.go`.
