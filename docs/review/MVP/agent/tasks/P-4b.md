---
id: P-4b
title: Guard test - every clear*Attempts store is cleared at logout
area: photographer
size: S
depends: []
edit:
  - apps/photographer/src/logout-clears-journals.test.ts (new)
context:
  - apps/photographer/src/auth-context.tsx:70-74
verify:
  - cd apps/photographer && npx vitest run src/logout-clears-journals.test.ts
---
## Goal
`auth-context.tsx` clears ~55 journals by name; a new journal that is forgotten there leaks one user's pending mutations to the next user. Make the omission a test failure.

## Spec
Test (node environment, `fs` only, no React): read all files `src/*.ts` and `src/*.tsx` (exclude `*.test.*`), collect every `export (async )?function (clear\w+)\(` whose name matches `/^clear\w*(Attempts?|Drafts?|Caches?|Bridges?|Storage|Jobs?)$/`; read `src/auth-context.tsx` and assert each collected name appears in it as `name(AsyncStorage, origin` or `name(AsyncStorage,origin`. Collect missing ones and fail with a message listing them. If the heuristic yields fewer than 40 names, fail with "scan broken". Add an explicit allow-list constant `NOT_PER_USER = []` (empty) for names that are intentionally not cleared, with a comment explaining it must be justified.
Run the test against the current tree: if it reports real missing names, keep the test failing - do not edit `auth-context.tsx` in this task.
