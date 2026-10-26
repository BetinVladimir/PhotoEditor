---
id: P-4e-catalog-create
title: Migrate catalog-create attempt store onto mutation-journal (pilot)
area: photographer
size: S
depends: [P-4d]
edit:
  - apps/photographer/src/catalog-create-attempt-store.ts
context:
  - apps/photographer/src/mutation-journal.ts
  - apps/photographer/src/catalog-create-attempt-store.ts
  - docs/review/MVP/agent/notes/P-4-catalog-create.md
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/catalog-create-attempt-store.test.ts
---
## Goal
Re-implement `catalog-create-attempt-store.ts` on top of `createJournal` from `src/mutation-journal.ts`, keeping its PUBLIC exports (class/function names, signatures, error codes, storage key strings) byte-for-byte compatible. The existing test `catalog-create-attempt-store.test.ts` is the characterization test and must pass UNCHANGED.

## Spec
- Keep the exported class/functions and types; their bodies delegate to the journal. Per-store pieces (request freezing, decode extras, conflict rules) stay in this file as hook functions, taken from the analysis note.
- Storage keys MUST NOT change (existing users have data under them).
- The module should shrink; if you cannot express a behaviour through the journal hooks, answer `BLOCKED: <behaviour>` instead of copying journal logic back.

## Do not
- Do not edit the test, `mutation-journal.ts`, or other stores.
