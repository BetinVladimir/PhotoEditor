---
id: P-1b
title: mvp-profile - isRouteEnabled helper
area: photographer
size: XS
depends: []
edit:
  - apps/photographer/src/mvp-profile.ts
  - apps/photographer/src/mvp-profile.test.ts
context:
  - apps/photographer/src/mvp-profile.ts
  - apps/photographer/src/mvp-profile.test.ts
verify:
  - cd apps/photographer && npx vitest run src/mvp-profile.test.ts
---
## Spec
Add to `mvp-profile.ts`: `export function isRouteEnabled(path: string, enabled = mvpProfileEnabled()): boolean { return !isDeferredInMvp(path, enabled); }`. Add a test `routes used by screens are enabled or disabled consistently`: `isRouteEnabled('/interchange/import-format', true) === false`, `isRouteEnabled('/styles/[id]/training', true) === false`, `isRouteEnabled('/styles', true) === true`, and with `false` as second argument everything is enabled.
