---
id: B-6-plan
title: B-6 - merge chunk notes into an extraction plan
area: analysis
size: S
depends: [B-6-an01, B-6-an02, B-6-an03, B-6-an04, B-6-an05, B-6-an06, B-6-an07, B-6-an08, B-6-an09, B-6-an10]
edit:
  - docs/review/MVP/agent/notes/B-6-plan.md (new)
context:
  - docs/review/MVP/agent/notes/B-6-chunk01.md
  - docs/review/MVP/agent/notes/B-6-chunk02.md
  - docs/review/MVP/agent/notes/B-6-chunk03.md
  - docs/review/MVP/agent/notes/B-6-chunk04.md
  - docs/review/MVP/agent/notes/B-6-chunk05.md
  - docs/review/MVP/agent/notes/B-6-chunk06.md
  - docs/review/MVP/agent/notes/B-6-chunk07.md
  - docs/review/MVP/agent/notes/B-6-chunk08.md
  - docs/review/MVP/agent/notes/B-6-chunk09.md
  - docs/review/MVP/agent/notes/B-6-chunk10.md
verify:
  - test -s docs/review/MVP/agent/notes/B-6-plan.md
  - python3 docs/review/MVP/agent/tools/cards_from_plan.py docs/review/MVP/agent/notes/B-6-plan.md --dry-run
---
## Goal
Turn the chunk notes into an extraction plan for ApplyCoordinator.build (2318 lines). READ-ONLY analysis; the only output is `docs/review/MVP/agent/notes/B-6-plan.md`. A deterministic tool (`tools/cards_from_plan.py`) will turn the plan into cards, so the format below is strict.

## Output
A short rationale (<= 15 lines) followed by exactly one fenced block:
```plan
area: B-6
domain: backend
file: backend/internal/workflow/apply.go
lang: go
gate: B-6-plan
verify: <command 1>
verify: <command 2>
---
id | title | start-end | target file | name | signature/notes
```
Rules for the rows: ids `a1`, `a2`, ... ; each range is <= 150 lines, ranges do not overlap, lines are the ones of the ORIGINAL file; cut only at the "Cut points" listed in the notes; a range must not contain a `return`/`goto`/label that leaves it unless the notes say how it is handled (then write that in `notes`); `target file` is a new file path in the same directory or `-` to put the function in the same file; `signature/notes` lists parameters (from "Reads from earlier") and return values (from "Declares ... used after yes"). Use the verify commands: cd backend && go build ./... && go vet ./internal/workflow / cd backend && go test ./internal/workflow -count=1 .
Prerequisite: card B-6t (characterization test) must be green; mention in the rationale which stages it covers.
The check of this card runs `cards_from_plan.py --dry-run` on your file: fix the plan until it is accepted.
