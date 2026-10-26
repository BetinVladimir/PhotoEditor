---
id: T-1a
title: tests/e2e scaffold (Playwright project, config, env helper)
area: tests
size: S
depends: []
edit:
  - tests/e2e/package.json (new)
  - tests/e2e/playwright.config.ts (new)
  - tests/e2e/tsconfig.json (new)
  - tests/e2e/helpers/env.ts (new)
  - tests/e2e/specs/smoke.spec.ts (new)
  - tests/e2e/.gitignore (new)
context: []
verify:
  - cd tests/e2e && node -e "JSON.parse(require('fs').readFileSync('package.json','utf8'))"
  - cd tests/e2e && grep -q "projects" playwright.config.ts
---
## Goal
Skeleton only: it must be possible to run `npx playwright test --list` after `npm ci`. The stack itself (compose.e2e.yaml) is a separate task.

## Spec
- `package.json`: name `@photoeditor/e2e`, private, scripts `test` = `playwright test`, `list` = `playwright test --list`, `typecheck` = `tsc --noEmit`; devDependencies `@playwright/test` `1.49.1`, `@axe-core/playwright` `4.10.1`, `typescript` `5.9.3`, `@types/node` `24.10.0`.
- `helpers/env.ts`: exports `env = { appURL, portalURL, apiURL, mailpitURL }` from `E2E_APP_URL` (default `https://localhost:8443`), `E2E_PORTAL_URL` (`https://localhost:8444`), `E2E_API_URL` (= app URL), `E2E_MAILPIT_URL` (`http://localhost:8025`), plus `locales = ['ru','en','bg'] as const`.
- `playwright.config.ts`: `testDir: './specs'`, `fullyParallel: false`, `workers: 1`, `retries: process.env.CI ? 1 : 0`, `reporter: [['list'], ['html', {open: 'never'}]]`, `use: {trace: 'on-first-retry', ignoreHTTPSErrors: true}`, two projects `photographer` (testMatch `photographer/**/*.spec.ts`, baseURL `env.appURL`) and `portal` (testMatch `portal/**/*.spec.ts`, baseURL `env.portalURL`), plus `smoke` (testMatch `smoke.spec.ts`).
- `specs/smoke.spec.ts`: one test `api liveness` using the `request` fixture: GET `${env.apiURL}/health/live` expects 200 and JSON `{status:'ok'}`; test `portal landing reachable`: GET portal `/ru` status < 400.
- `.gitignore`: `node_modules`, `playwright-report`, `test-results`.
