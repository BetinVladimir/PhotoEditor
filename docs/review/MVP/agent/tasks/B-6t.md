---
id: B-6t
title: Characterization test for ApplyCoordinator.build (before the split)
area: backend
size: S
depends: []
edit:
  - backend/internal/workflow/apply_build_characterization_test.go (new)
context:
  - backend/internal/workflow/apply.go:834-870
verify:
  - cd backend && go vet ./internal/workflow
  - cd backend && go test ./internal/workflow -run TestApplyBuildCharacterization -count=1
---
## Goal
Pin what `ApplyCoordinator.build` observably does before it is cut into pieces (card B-6-plan). Use the fakes/test doubles that the existing tests in `backend/internal/workflow/*_test.go` already use (find them with `grep -n "func Test.*Apply" backend/internal/workflow/*_test.go`); do not add a database dependency if the existing tests avoid one.

## Spec
Table-driven `TestApplyBuildCharacterization`: for each of the existing apply scenarios you can reach with those fakes (success, rejected input, stale lease, cancelled), call `build` and assert the returned `(bool, error)` and the recorded side effects (calls on the fake store, in order). Record the CURRENT behaviour, even if it looks odd; add a comment `// characterization, not a requirement`.
If fewer than 3 scenarios can be built without a real database, write those and add `t.Skip("needs database: <scenario>")` entries for the rest, then answer with the list of skipped scenarios.

## Do not
- Do not modify `apply.go` or other production code.
