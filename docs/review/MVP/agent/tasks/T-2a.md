---
id: T-2a
title: e2e spec E-16 - invalid portal secret shows an invalid state without data leakage
area: tests
size: S
depends: [T-1a]
edit:
  - tests/e2e/specs/portal/invalid-secret.spec.ts (new)
context:
  - apps/portal/src/lib/api.ts::secretFromFragment
  - apps/portal/src/components/Portal.tsx:25-37
  - apps/portal/src/components/Portal.tsx:330-345
verify:
  - cd tests/e2e && npx tsc --noEmit
  - cd tests/e2e && npx playwright test --list
---
## Goal
Scenario E-16 from the plan. It must run against a live stack later; for this task it only has to compile and be listed.

## Spec
Playwright test file with three tests under `test.describe('portal invalid links')`:
1. `short secret in fragment -> invalid screen`: `page.goto('/ru/s/48cf689a-c8c0-4f70-83a7-982fd95fd946#k=short')`; expect the page shows the invalid-state UI. Do not guess texts: assert on the ABSENCE of any gallery (`page.locator('img')` count 0) and that NO request to `/api/v1/portal/sessions` was made (collect with `page.on('request')`), because `secretFromFragment` rejects short secrets client-side.
2. `well-formed but unknown secret -> invalid, no images`: secret `'a'.repeat(32)`; the session request returns 401/404 (allow either); assert `img` count 0 and that the response bodies of `/api/v1/portal/sessions` contain no `collection` key.
3. `secret never leaves the browser in URLs`: for a valid-looking secret collect all request URLs; none may contain the secret string; the `Referrer-Policy` response header of the document is `no-referrer`.
Use the three locales via `for (const locale of locales)` only in test 1. Import `locales` from `../../helpers/env`.
