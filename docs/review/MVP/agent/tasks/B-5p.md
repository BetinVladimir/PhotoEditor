---
id: B-5p
title: Hoist the draftsV2 Media configuration next to its declaration
area: backend
size: XS
depends: [B-5a]
edit:
  - backend/internal/httpapi/catalog.go
context:
  - backend/internal/httpapi/catalog.go:103-170
verify:
  - cd backend && go build ./... && go vet ./internal/httpapi
  - cd backend && go test ./internal/httpapi -run TestRoutesGolden -count=1
---
## Goal
`catalogRoutes` (backend/internal/httpapi/catalog.go) configures the local `draftsV2` in the middle of the function (`if s.Media != nil { draftsV2.Objects, _ = ...; draftsV2.Bucket = ... }`). A later mechanical card moves route blocks into methods that receive `draftsV2` BY VALUE, so this configuration must happen right after the declaration, before any block.

## Spec
1. Find the declaration `draftsV2 := v2draft.Service{DB: s.DB, Cursors: people.Cursors}`.
2. Find the 4-line block that starts with `if s.Media != nil {` and contains exactly the two statements `draftsV2.Objects, _ = s.Media.Objects.(v2draft.DraftStore)` and `draftsV2.Bucket = s.Media.Bucket` (it is located a few hundred lines later, right before the call `s.editorCapabilityRoutes(r,runs,draftsV2)`).
3. Cut that block and paste it immediately AFTER the `draftsV2 := ...` declaration line. Do not change anything else. Do not reformat the file.

## Do not
- Do not touch other `if s.Media != nil` blocks. Do not reorder any route registrations.
