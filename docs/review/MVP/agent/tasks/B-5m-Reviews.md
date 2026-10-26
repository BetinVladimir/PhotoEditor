---
id: B-5m-Reviews
title: Move catalogRoutes block "reviews and result selections" into catalogReviewsRoutes
area: backend
size: XS
depends: [B-5a]
edit:
  - backend/internal/httpapi/catalog.go
  - backend/internal/httpapi/catalog_reviews.go (new)
context:
  - backend/internal/httpapi/catalog.go:103-170
verify:
  - cd backend && go build ./... && go vet ./internal/httpapi
  - cd backend && go test ./internal/httpapi -run TestRoutesGolden -count=1
  - test -f backend/internal/httpapi/catalog_reviews.go
---
## Goal
Move the block of `catalogRoutes` that registers: reviews and result selections into its own method `catalogReviewsRoutes` in `backend/internal/httpapi/catalog_reviews.go`. This is a pure cut-and-paste refactor; behaviour must not change. A deterministic script does the edit; you only run it and fix the build if needed. DO NOT retype or rewrite route handlers by hand.

## Steps
1. Dry run (writes nothing):
```
python3 docs/review/MVP/agent/tools/move_routes.py --name Reviews \
    --first 'r.Get("/api/v1/tenants/{t}/reviews"' \
    --last  'r.Post("/api/v1/tenants/{t}/reviews/{id}/decision"' \
    --dry-run
```
It prints the line range, the inferred parameters and the call. If it prints `REFUSED: ...`, stop and answer `BLOCKED: <the message>`.
2. Run the same command without `--dry-run`.
3. `cd backend && go build ./... && go vet ./internal/httpapi`. Typical fixes (and ONLY these):
   - `imported and not used` / `undefined: pkg` in `catalog.go` or `catalog_reviews.go`: adjust the import lists (copy the import line from the other file).
   - `copylocks` from vet: change the parameter type in the new function to a pointer (`*pkg.Type`) and pass `&name` in the call.
4. `cd backend && go test ./internal/httpapi -run TestRoutesGolden -count=1` must pass (the route table is unchanged).

## Do not
- Do not edit route paths, handlers, middleware or the order of the calls in `catalogRoutes`.
- Do not run `gofmt -w` on `catalog.go` (it would reformat unrelated code).
- Do not create more than the one new file `catalog_reviews.go`.
