---
id: T-1b
title: e2e helpers - Mailpit OTP reader and API client
area: tests
size: S
depends: [T-1a]
edit:
  - tests/e2e/helpers/mail.ts (new)
  - tests/e2e/helpers/api.ts (new)
  - tests/e2e/helpers/helpers.unit.test.ts (new)
context:
  - tests/e2e/helpers/env.ts
  - apps/photographer/src/auth.ts:1-60
verify:
  - cd tests/e2e && npx tsc --noEmit
---
## Goal
Login in e2e is OTP over e-mail. Provide the building blocks used by every spec.

## Spec
- `mail.ts`: `export async function waitForOTP(request: APIRequestContext, email: string, timeoutMs = 15000): Promise<string>` polls Mailpit `GET {mailpitURL}/api/v1/search?query=to:{email}` every 500 ms, picks the newest message, fetches `GET /api/v1/message/{ID}` and extracts the first 6-digit code with `/\b(\d{6})\b/` from `Text`; throws a clear error on timeout. `export async function clearMailbox(request)` -> `DELETE /api/v1/messages`.
- `api.ts`: `export async function csrf(request, base)` -> `GET {base}/api/v1/auth/csrf` returns `{csrf_token}` (see how `auth.ts (OwnerAuth) and screens/login.tsx` calls it; mirror header names exactly), and `export async function loginViaAPI(request, email): Promise<{csrf: string}>` doing `otp/request` then `waitForOTP` then `otp/verify`. Read endpoint paths and JSON field names ONLY from the shown `auth.ts (OwnerAuth) and screens/login.tsx`; if a field is not visible answer `BLOCKED: need request shapes`.
- `helpers.unit.test.ts`: not a Playwright test; skip it in Playwright config by naming (`*.unit.test.ts` is not matched by `testMatch`). Put a pure function `extractCode(text: string): string | null` in `mail.ts` and test it with `node --test`-compatible code is NOT required: simply export `extractCode` and skip the unit test file (delete it from the edit list if you do not write it).
