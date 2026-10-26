---
id: P-9-plan
title: P-9 - merge chunk notes into an extraction plan
area: analysis
size: S
depends: [P-9-an01, P-9-an02, P-9-an03, P-9-an04, P-9-an05, P-9-an06, P-9-an07, P-9-an08, P-9-an09]
edit:
  - docs/review/MVP/agent/notes/P-9-plan.md (new)
context:
  - docs/review/MVP/agent/notes/P-9-chunk01.md
  - docs/review/MVP/agent/notes/P-9-chunk02.md
  - docs/review/MVP/agent/notes/P-9-chunk03.md
  - docs/review/MVP/agent/notes/P-9-chunk04.md
  - docs/review/MVP/agent/notes/P-9-chunk05.md
  - docs/review/MVP/agent/notes/P-9-chunk06.md
  - docs/review/MVP/agent/notes/P-9-chunk07.md
  - docs/review/MVP/agent/notes/P-9-chunk08.md
  - docs/review/MVP/agent/notes/P-9-chunk09.md
verify:
  - test -s docs/review/MVP/agent/notes/P-9-plan.md
  - python3 docs/review/MVP/agent/tools/cards_from_plan.py docs/review/MVP/agent/notes/P-9-plan.md --dry-run
---
## Goal
Turn the chunk notes into an extraction plan for the 894-line editor screen. READ-ONLY analysis; the only output is `docs/review/MVP/agent/notes/P-9-plan.md`. A deterministic tool (`tools/cards_from_plan.py`) will turn the plan into cards, so the format below is strict.

## Output
A short rationale (<= 15 lines) followed by exactly one fenced block:
```plan
area: P-9
domain: photographer
file: apps/photographer/screens/editor/[version].tsx
lang: ts
gate: P-9-plan
verify: <command 1>
verify: <command 2>
---
id | title | start-end | target file | name | signature/notes
```
Rules for the rows: ids `a1`, `a2`, ... ; each range is <= 150 lines, ranges do not overlap, lines are the ones of the ORIGINAL file; cut only at the "Cut points" listed in the notes; a range must not contain a `return`/`goto`/label that leaves it unless the notes say how it is handled (then write that in `notes`); `target file` is a new file path in the same directory or `-` to put the function in the same file; `signature/notes` lists parameters (from "Reads from earlier") and return values (from "Declares ... used after yes"). Use the verify commands: cd apps/photographer && npx tsc --noEmit / cd apps/photographer && npx vitest run .
Targets live in `apps/photographer/screens/editor/` (hooks `use*.ts`, panels `*Panel.tsx`); keep the default export in the original file.
The check of this card runs `cards_from_plan.py --dry-run` on your file: fix the plan until it is accepted.
