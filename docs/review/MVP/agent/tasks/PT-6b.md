---
id: PT-6b
title: Rate-limit notice - pure helper and i18n keys
area: portal
size: S
depends: [PT-6a]
edit:
  - apps/portal/src/lib/error-notice.ts (new)
  - apps/portal/src/lib/error-notice.test.ts (new)
  - apps/portal/src/catalogs/en.json
  - apps/portal/src/catalogs/ru.json
  - apps/portal/src/catalogs/bg.json
context:
  - apps/portal/src/lib/i18n.ts
verify:
  - cd apps/portal && npx vitest run src/lib/error-notice.test.ts
---
## Goal
Show "too many requests, try again in N s" instead of the generic error.

## Spec
1. Catalogs: add key `rate_limited` to the three files, value with a `{seconds}` placeholder - en: `Too many requests. Try again in {seconds} s.`; ru: `Слишком много запросов. Повторите через {seconds} с.`; bg: `Твърде много заявки. Опитайте отново след {seconds} с.` (read `lib/i18n.ts` to see how catalogs are merged into `messages` and whether keys must also be added to a TypeScript type).
2. `error-notice.ts`: `export function noticeFor(error: unknown, t: Record<string,string>): string | null` - returns `null` when the error is not an `APIError` with status 429; otherwise `t.rate_limited` with `{seconds}` replaced by `String(error.retryAfterSeconds ?? 30)`.
3. Test: 429 with 12 -> contains `12`; 429 without header -> `30`; 500 and non-APIError -> `null`; placeholder fully replaced for en/ru/bg catalogs imported from `./i18n` (`messages.en`, ...).
