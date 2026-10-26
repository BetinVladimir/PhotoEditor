---
id: P-4d
title: Mutation journal core module (src/mutation-journal.ts) from the approved contract
area: photographer
size: S
depends: [P-4c]
edit:
  - apps/photographer/src/mutation-journal.ts (new)
  - apps/photographer/src/mutation-journal.test.ts (new)
context:
  - docs/review/MVP/agent/notes/P-4-contract.md
  - apps/photographer/src/catalog-create-attempt-store.ts
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/mutation-journal.test.ts
---
## Goal
Implement the generic journal exactly as specified by `notes/P-4-contract.md`.

## Step 0 (gate)
Run `grep -n "^APPROVED-BY:" docs/review/MVP/agent/notes/P-4-contract.md`. If the line is missing or still says `(nobody yet)`, STOP and answer `BLOCKED: contract not approved by a human`.

## Spec
- `src/mutation-journal.ts` exports what the contract section "Core API" lists, nothing more. Storage key layout, size limit, epoch/generation/queue/lock semantics: copy from the contract section 1 literally; where the contract is silent, mirror `catalog-create-attempt-store.ts`.
- `src/mutation-journal.test.ts` (vitest): one test per numbered item of contract section 1 (fake in-memory `DraftStorage`; look at how `catalog-create-attempt-store.test.ts` builds one), plus: two journals with different `kind` do not see each other's records; `clearAll` removes only the keys of the given origin.

## Do not
- Do not touch any existing store. Do not add dependencies.
