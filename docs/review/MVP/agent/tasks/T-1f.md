---
id: T-1f
title: e2e helper - seed (user session, tenant, project, collection)
area: tests
size: S
depends: [T-1b, T-2e02r]
edit:
  - tests/e2e/helpers/seed.ts (new)
context:
  - tests/e2e/helpers/api.ts
  - tests/e2e/helpers/mail.ts
  - docs/review/MVP/agent/notes/E-02.md
verify:
  - cd tests/e2e && npx tsc --noEmit
---
## Goal
Every spec needs an isolated user, tenant, project and collection. Provide typed helpers on top of `api.ts`, using ONLY the request shapes recorded in `notes/E-02.md` (recon of scenario E-02).

## Spec (`helpers/seed.ts`)
- `export interface Session { email: string; csrf: string; request: APIRequestContext }`.
- `export async function newUser(playwright: Playwright, prefix = 'e2e'): Promise<Session>` - unique e-mail `${prefix}+${randomUUID()}@example.test`, creates an `APIRequestContext` with `ignoreHTTPSErrors`, calls `loginViaAPI`.
- `export async function createTenant(s: Session, name?: string): Promise<{ id: string; revision?: string }>`, `createProject(s, tenant, name?)`, `createCollection(s, tenant, project, name?)` - each sends a fresh `Idempotency-Key: randomUUID()` and the CSRF header as `api.ts` does, asserts the success status from the note, returns the parsed JSON (typed with only the fields the note lists; extra fields as `unknown`).
- `export async function invite(...)` is NOT part of this card.
If a request shape in the note is marked `UNKNOWN:`, throw `new Error('seed: UNKNOWN <what>')` in that function instead of guessing.

## Do not
- Do not call endpoints that are not in `notes/E-02.md`.
