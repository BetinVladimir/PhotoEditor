---
id: B-7-plan
title: B-7 - merge chunk notes into an extraction plan
area: analysis
size: S
depends: [B-7-an01, B-7-an02, B-7-an03, B-7-an04]
edit:
  - docs/review/MVP/agent/notes/B-7-plan.md (new)
context:
  - docs/review/MVP/agent/notes/B-7-chunk01.md
  - docs/review/MVP/agent/notes/B-7-chunk02.md
  - docs/review/MVP/agent/notes/B-7-chunk03.md
  - docs/review/MVP/agent/notes/B-7-chunk04.md
verify:
  - test -s docs/review/MVP/agent/notes/B-7-plan.md
  - python3 docs/review/MVP/agent/tools/cards_from_plan.py docs/review/MVP/agent/notes/B-7-plan.md --dry-run
---
## Goal
Turn the chunk notes into an extraction plan for runWithAPIDependencies (640 lines). READ-ONLY analysis; the only output is `docs/review/MVP/agent/notes/B-7-plan.md`. A deterministic tool (`tools/cards_from_plan.py`) will turn the plan into cards, so the format below is strict.

## Output
A short rationale (<= 15 lines) followed by exactly one fenced block:
```plan
area: B-7
domain: backend
file: backend/cmd/photo/main.go
lang: go
gate: B-7-plan
verify: <command 1>
verify: <command 2>
---
id | title | start-end | target file | name | signature/notes
```
Rules for the rows: ids `a1`, `a2`, ... ; each range is <= 150 lines, ranges do not overlap, lines are the ones of the ORIGINAL file; cut only at the "Cut points" listed in the notes; a range must not contain a `return`/`goto`/label that leaves it unless the notes say how it is handled (then write that in `notes`); `target file` is a new file path in the same directory or `-` to put the function in the same file; `signature/notes` lists parameters (from "Reads from earlier") and return values (from "Declares ... used after yes"). Use the verify commands: cd backend && go build ./... && go vet ./cmd/photo / cd backend && go test ./cmd/photo -count=1 .

The check of this card runs `cards_from_plan.py --dry-run` on your file: fix the plan until it is accepted.
