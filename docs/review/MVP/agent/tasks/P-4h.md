---
id: P-4h
title: Analysis - migration recipe from the three pilot diffs
area: photographer
size: S
depends: [P-4e-catalog-create, P-4e-collection-create, P-4e-export-batch]
edit:
  - docs/review/MVP/agent/notes/P-4-recipe.md (new)
context:
  - docs/review/MVP/agent/notes/P-4-contract.md
verify:
  - test -s docs/review/MVP/agent/notes/P-4-recipe.md
---
## Goal
Distil the three pilot migrations into a step-by-step recipe that a small model can follow for the other 38 stores. READ-ONLY analysis, one markdown file, <= 80 lines.

## Steps
1. Run `git log --oneline --grep "agent(P-4e-"` and for each of the three commits `git show --stat <sha>` and `git show <sha> -- apps/photographer/src/*-attempt-store.ts`.
2. Write the recipe: `## Before you start` (what to read), `## Mapping` (table: old construct in a store -> journal option/hook), `## Steps` (numbered, mechanical, each checkable), `## Known pitfalls` (from the diffs: e.g. key prefixes, error codes), `## When to answer BLOCKED`.
Use concrete identifiers from the diffs only.
