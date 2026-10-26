---
id: PT-3t
title: Tests - caller-supplied Idempotency-Key for download intents
area: portal
size: XS
depends: []
edit:
  - apps/portal/src/lib/api.test.ts
context:
  - apps/portal/src/lib/api.ts::redeemDownload
  - apps/portal/src/lib/api.ts::downloadItem
  - apps/portal/src/lib/api.test.ts:1-8
verify:
  - cd apps/portal && npx vitest run src/lib/api.test.ts
---
## Spec
Add a `describe("download idempotency keys")` block to `api.test.ts`. Stub `globalThis.fetch` with `vi.fn()` that returns, for the first call (`.../download-intent`), `new Response(JSON.stringify({intent_token: X}), {status:201})` where `X` is whatever valid token shape `validateDownloadIntent` accepts (read `apps/portal/src/lib/download-link.ts` tests for a valid sample: if unsure, make the SECOND call fail by rejecting the promise - the test only inspects the FIRST request's headers, so `await expect(...).rejects.toBeDefined()` is fine).
Assertions on the first fetch call's `init.headers['Idempotency-Key']`:
1. `redeemDownload('csrf','job',1,undefined,'key-1')` sends `key-1`.
2. `downloadItem('csrf','item','preset',undefined,'key-2')` sends `key-2`.
3. Without the last argument both send a UUID-shaped value, and two calls send different values.
Import `vi` from vitest; restore fetch in `afterEach`.
