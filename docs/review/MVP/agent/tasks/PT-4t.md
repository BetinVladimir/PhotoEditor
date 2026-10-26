---
id: PT-4t
title: Characterization test for Portal (jsdom, stubbed fetch) before decomposition
area: portal
size: S
depends: [PT-4a]
edit:
  - apps/portal/src/components/Portal.test.tsx (new)
context:
  - apps/portal/src/components/AccessibleLightbox.test.tsx:1-14
  - docs/review/MVP/agent/notes/PT-4-state-map.md
  - apps/portal/src/lib/api.ts:1-40
verify:
  - cd apps/portal && npx vitest run src/components/Portal.test.tsx
---
## Goal
A safety net for the refactor PT-4b..PT-4i: render the real `Portal` in jsdom with `fetch` stubbed and pin the observable behaviour. Same technique as `AccessibleLightbox.test.tsx` (JSDOM + `createRoot` + `act`; no testing-library in this package).

## Spec (4-6 tests, each independent)
1. Valid share: stub `fetch` so the session exchange and the first gallery page succeed (read `src/lib/api.ts` for the URLs and response shapes it expects); expect the gallery heading and N items rendered.
2. Invalid share (`404`): expect the invalid/unavailable state text from `messages` and no gallery.
3. `refuseDownloadAccess` behaviour: after a session `403/410` the gallery is cleared and no further `fetch` call is made with the old session (count calls).
4. Selecting an item toggles its selected state (aria-pressed or the attribute the component uses).
5. Unmount aborts in-flight requests (the stub records `signal.aborted`).
Use `Locale` `en` and `messages.en` strings by importing them; do not hard-code UI text.
If a scenario cannot be driven without changing `Portal.tsx`, skip it with `it.skip` and a one-line reason; do not edit the component.

## Do not
- Do not edit `Portal.tsx` or any library file. Do not add dependencies.
