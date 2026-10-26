---
id: P-1f
title: MVP guard for the non-localized routes in app/_layout.tsx
area: photographer
size: XS
depends: [P-1b]
edit:
  - apps/photographer/app/_layout.tsx
  - apps/photographer/src/mvp-profile.test.ts
context:
  - apps/photographer/app/_layout.tsx
  - apps/photographer/app/[locale]/_layout.tsx
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/mvp-profile.test.ts
---
## Goal
`app/interchange/*`, `app/exports/sign.tsx` etc. (routes WITHOUT the locale prefix) bypass the guard that exists only in `app/[locale]/_layout.tsx`.

## Spec
In `app/_layout.tsx`'s `RootLayout`: call `usePathname()` (import from `expo-router`, next to `Stack`) and, if `isDeferredInMvp(pathname)` is true, return `<PhotographerPaperProvider><LocaleProvider><Text accessibilityRole="alert">{...}</Text></LocaleProvider></PhotographerPaperProvider>` using a plain message: because `useLocale` is only valid inside `LocaleProvider`, create a tiny inner component `DeferredNotice` that calls `useLocale()` and renders `t('feature_not_in_mvp')` (the key exists in all locales). Import `Text` from `react-native-paper`. Hooks must be called unconditionally at the top of `RootLayout`.
Extend `mvp-profile.test.ts`: `isDeferredInMvp('/interchange/new', true)` and `('/exports/sign', true)` are true without a locale prefix.
