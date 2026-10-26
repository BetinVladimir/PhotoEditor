---
id: PT-6a
title: APIError.retryAfterSeconds from the Retry-After header
area: portal
size: XS
depends: []
edit:
  - apps/portal/src/lib/api.ts
  - apps/portal/src/lib/api.test.ts
context:
  - apps/portal/src/lib/api.ts:1-18
verify:
  - cd apps/portal && npx vitest run src/lib/api.test.ts
---
## Spec
1. `APIError` gets a fourth constructor parameter `public retryAfterSeconds?: number`.
2. In `decode`, parse `response.headers.get("Retry-After")`: only a decimal integer 1..3600 is accepted (HTTP-date form and anything else -> `undefined`); pass it to `new APIError(...)` as the fourth argument.
3. Export a pure helper `parseRetryAfter(value: string | null): number | undefined` used by `decode`.
## Tests (append to `api.test.ts`)
`parseRetryAfter`: `"30"`->30, `"0"`->undefined, `"99999"`->undefined, `"Wed, 21 Oct 2026 07:28:00 GMT"`->undefined, `null`->undefined, `" 7 "`->7. One `decode` integration test through a stubbed `fetch` returning status 429 with `Retry-After: 12` and JSON body `{}` for any exported API function that does a plain GET (e.g. the one used for the collection) and assert the thrown `APIError` has `status 429` and `retryAfterSeconds 12`.
