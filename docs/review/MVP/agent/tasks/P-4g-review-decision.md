---
id: P-4g-review-decision
title: Migrate review-decision attempt store onto mutation-journal
area: photographer
size: S
depends: [P-4f]
edit:
  - apps/photographer/src/review-decision-attempt-store.ts
context:
  - apps/photographer/src/mutation-journal.ts
  - apps/photographer/src/review-decision-attempt-store.ts
  - docs/review/MVP/agent/notes/P-4-recipe.md
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/review-decision-attempt-store.test.ts
---
## Goal
Follow `notes/P-4-recipe.md` to move `review-decision-attempt-store.ts` onto `createJournal`. Public exports, storage keys and error codes stay identical.

## Do not
- Do not edit other files (except the new test above). If the recipe does not cover a construct in this store, answer `BLOCKED: <construct>`.
