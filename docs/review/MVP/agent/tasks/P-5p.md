---
id: P-5p
title: OwnerAPI: make members public and move requireArchiveUUID to api/shared.ts (mechanical)
area: photographer
size: XS
depends: []
edit:
  - apps/photographer/src/owner-api.ts
  - apps/photographer/src/api/shared.ts (new)
context:
  - apps/photographer/src/owner-api.ts:118-124
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/owner-api
  - test -f apps/photographer/src/api/shared.ts
---
## Goal
Preparation for splitting `class OwnerAPI` (about 200 methods in one 1079-line file) into per-domain modules. Moved methods become plain functions `fn(self: OwnerAPI, ...)`, so the class members they use (`client`, `tenant`, `path`, `get`, `mutate`, ...) must be public, and the module-level helper must live in a shared file. A deterministic script does it; you only run it.

## Steps
1. Dry run: `python3 docs/review/MVP/agent/tools/move_methods.py --prepare --dry-run`. If it prints `REFUSED: ...` answer `BLOCKED: <message>`.
2. Run: `python3 docs/review/MVP/agent/tools/move_methods.py --prepare`.
3. `cd apps/photographer && npx tsc --noEmit` must be clean. If it reports an unused import of `archiveUUID` in `owner-api.ts` that is NOT the case (it is still used elsewhere); do not touch imports.
4. `cd apps/photographer && npx vitest run src/owner-api` must pass.

## Do not
- Do not edit any method body or any other file. Do not run a formatter.
