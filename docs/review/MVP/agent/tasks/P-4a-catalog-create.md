---
id: P-4a-catalog-create
title: Analysis - catalog-create attempt store (input for the mutation journal)
area: photographer
size: S
depends: []
edit:
  - docs/review/MVP/agent/notes/P-4-catalog-create.md (new)
context:
  - apps/photographer/src/catalog-create-attempt-store.ts
verify:
  - test -s docs/review/MVP/agent/notes/P-4-catalog-create.md
---
## Goal
READ-ONLY analysis. Produce a compact, accurate description of how this attempt store works, as input for a shared "mutation journal" module (plan item P-4). Output ONE markdown file, <= 90 lines. Do not edit code.

## Output sections (exactly these)
1. `## Storage`: every storage key prefix / generation key (literal strings), how the key is built, max stored size.
2. `## Record shape`: the TypeScript type of the stored attempt and of the request it freezes; list the field validations in `freezeRequest`/`decode` (one line per rule).
3. `## Operations`: table `method | what it does (<= 12 words) | errors thrown (codes)`. Include the module-level `clear*Attempts` function.
4. `## Concurrency invariants`: how `epoch` / `epochs` map, the `generation` fence, `queue`/`pending`, and the external `lock` are used and in what order (as a numbered list).
5. `## Specific to this store`: bullet list of everything that differs from a generic "freeze request -> write -> resume -> complete -> clear" journal (extra fields, ticket/receipt rules, conflict rules).
Copy names and error codes literally from the code; if something is not visible write `(not visible)`.
