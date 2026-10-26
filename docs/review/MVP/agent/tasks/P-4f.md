---
id: P-4f
title: Journal registry - logout clears every registered journal (and a test proves it)
area: photographer
size: S
depends: [P-4h]
edit:
  - apps/photographer/src/mutation-journal.ts
  - apps/photographer/src/auth-context.tsx
  - apps/photographer/src/journal-registry.test.ts (new)
context:
  - apps/photographer/src/auth-context.tsx:60-90
  - docs/review/MVP/agent/notes/P-4-contract.md
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/journal-registry.test.ts src/logout-clears-journals.test.ts
---
## Goal
A journal created through `createJournal` registers itself in a module-level registry, so logout cannot forget it.

## Spec
- `mutation-journal.ts`: export `journalRegistry` (read-only view: kinds) and `clearAllJournals(storage, origin): Promise<void>` which calls the `clear` of every registered journal (sequentially, errors collected and rethrown as one `AggregateError`).
- `auth-context.tsx`: next to the existing `Promise.all([...clear*Attempts])` call (about line 74) add `await clearAllJournals(AsyncStorage, origin)`; keep all existing calls for now.
- Test: create two journals with fake storage, write one record each, call `clearAllJournals`, expect both empty; a journal whose `clear` throws does not prevent the other from being cleared and the error is reported.

## Do not
- Do not remove the existing clear calls (stores not migrated yet still need them).
