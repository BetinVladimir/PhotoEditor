---
id: B-5a
title: Golden test of the HTTP route table (safety net for splitting catalogRoutes)
area: backend
size: S
depends: []
edit:
  - backend/internal/httpapi/routes_golden_test.go (new)
context:
  - backend/internal/httpapi/server.go::Server.Routes
verify:
  - cd backend && go test ./internal/httpapi -run 'TestRoutesGolden' -update -count=1
  - cd backend && go test ./internal/httpapi -run 'TestRoutesGolden' -count=1
---
## Goal
Before any refactoring of the 3800-line `catalogRoutes`, record every registered `METHOD pattern` so that the refactoring can be proven behaviour-neutral.

## Spec
`routes_golden_test.go` (package `httpapi`):
- `var update = flag.Bool("update", false, "rewrite testdata/routes.golden")`.
- `TestRoutesGolden`: build `(&Server{}).Routes()` (other tests in the package construct `Server` the same way - if `Routes` panics on nil services, set only the fields it dereferences while building routes; handlers are never invoked), `chi.Walk(router.(chi.Routes), func(method, route string, ...) error { lines = append(lines, method+" "+route); return nil })`, sort, join with `\n` plus a trailing newline.
- Compare to `testdata/routes.golden`; with `-update` write it (create the dir). On mismatch print a unified-style diff of added/removed lines (simple set difference is enough).
- Also assert there are no duplicate `METHOD route` lines.
