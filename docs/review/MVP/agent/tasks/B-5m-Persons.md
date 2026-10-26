---
id: B-5m-Persons
title: Move catalogRoutes block "project persons" into catalogPersonsRoutes
area: backend
size: XS
depends: [B-5a]
edit:
  - backend/internal/httpapi/catalog.go
  - backend/internal/httpapi/catalog_persons.go (new)
context:
  - backend/internal/httpapi/catalog.go:103-170
verify:
  - cd backend && go build ./... && go vet ./internal/httpapi
  - cd backend && go test ./internal/httpapi -run TestRoutesGolden -count=1
  - test -f backend/internal/httpapi/catalog_persons.go
---
## Goal
Move the block of `catalogRoutes` that registers: project persons, consents, observations (+ the persons/consents listing loop) into its own method `catalogPersonsRoutes` in `backend/internal/httpapi/catalog_persons.go`. This is a pure cut-and-paste refactor; behaviour must not change. A deterministic script does the edit; you only run it and fix the build if needed. DO NOT retype or rewrite route handlers by hand.

## Steps
1. Dry run (writes nothing):
```
python3 docs/review/MVP/agent/tools/move_routes.py --name Persons \
    --first 'for _, listing := range []struct{ path, kind string }{' \
    --last  'r.Post("/api/v1/tenants/{t}/project-persons/{id}/observations"' \
    --dry-run
```
It prints the line range, the inferred parameters and the call. If it prints `REFUSED: ...`, stop and answer `BLOCKED: <the message>`.
2. Run the same command without `--dry-run`.
3. `cd backend && go build ./... && go vet ./internal/httpapi`. Typical fixes (and ONLY these):
   - `imported and not used` / `undefined: pkg` in `catalog.go` or `catalog_persons.go`: adjust the import lists (copy the import line from the other file).
   - `copylocks` from vet: change the parameter type in the new function to a pointer (`*pkg.Type`) and pass `&name` in the call.
4. `cd backend && go test ./internal/httpapi -run TestRoutesGolden -count=1` must pass (the route table is unchanged).

## Do not
- Do not edit route paths, handlers, middleware or the order of the calls in `catalogRoutes`.
- Do not run `gofmt -w` on `catalog.go` (it would reformat unrelated code).
- Do not create more than the one new file `catalog_persons.go`.
