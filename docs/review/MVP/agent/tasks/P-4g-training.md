---
id: P-4g-training
title: Migrate training attempt store onto mutation-journal
area: photographer
size: S
depends: [P-4f]
edit:
  - apps/photographer/src/training-attempt-store.ts
  - apps/photographer/src/training-attempt-store.test.ts (new)
context:
  - apps/photographer/src/mutation-journal.ts
  - apps/photographer/src/training-attempt-store.ts
  - docs/review/MVP/agent/notes/P-4-recipe.md
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/logout-clears-journals.test.ts
---
## Goal
Follow `notes/P-4-recipe.md` to move `training-attempt-store.ts` onto `createJournal`. Public exports, storage keys and error codes stay identical.
This store has NO test of its own: before the change, write the smallest characterization test `src/training-attempt-store.test.ts` (write -> read returns the same record; clear removes it), run it green on the OLD code, then migrate.

## Do not
- Do not edit other files (except the new test above). If the recipe does not cover a construct in this store, answer `BLOCKED: <construct>`.
