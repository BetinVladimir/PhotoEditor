---
id: P-4c
title: Analysis - common contract of the three pilot stores (mutation journal spec)
area: photographer
size: S
depends: [P-4a-catalog-create, P-4a-collection-create, P-4a-export-batch]
edit:
  - docs/review/MVP/agent/notes/P-4-contract.md (new)
context:
  - docs/review/MVP/agent/notes/P-4-catalog-create.md
  - docs/review/MVP/agent/notes/P-4-collection-create.md
  - docs/review/MVP/agent/notes/P-4-export-batch.md
verify:
  - test -s docs/review/MVP/agent/notes/P-4-contract.md
---
## Goal
Merge the three analysis notes into ONE contract for `src/mutation-journal.ts`. READ-ONLY analysis, output one markdown file, <= 120 lines.

## Output sections (exactly these)
1. `## Common behaviour`: what all three stores do identically (storage, epoch, generation fence, queue, lock order, size limit). Numbered, precise.
2. `## Core API`: proposed TypeScript signatures: `createJournal<Req,Rec>(options)`, the options object (every field with a type and a one-line meaning), and the returned object's methods. Every behaviour of section 1 must be reachable from these signatures.
3. `## Hooks per store`: what each pilot must still supply itself (freeze, decode extras, conflict predicates), as a table store x hook.
4. `## Behaviour changes`: any place where a generic implementation would change observable behaviour (error codes, ordering). If none, write `none`.
5. `## Open questions for the human reviewer`: <= 8 items, each answerable yes/no or by choosing one option.
6. A last line exactly: `APPROVED-BY: (nobody yet)`.
Do not invent behaviour that is not in the notes. A human replaces the last line with `APPROVED-BY: <name>` after review; card P-4d refuses to run without it.
