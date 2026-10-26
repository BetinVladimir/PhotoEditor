---
id: PT-6c
title: Use noticeFor in Portal download paths
area: portal
size: XS
depends: [PT-6b]
edit:
  - apps/portal/src/components/Portal.tsx
context:
  - apps/portal/src/components/Portal.tsx:249-272
  - apps/portal/src/components/Portal.tsx:297-313
verify:
  - cd apps/portal && npx tsc --noEmit
  - cd apps/portal && npx vitest run src/components
---
## Spec
Import `noticeFor` from `../lib/error-notice`. In the two `catch` blocks shown (`createDownload` and `openDownload`) where the code calls `setNotice(t.error)` after the access-refusal check, change it to `setNotice(noticeFor(error, t as unknown as Record<string,string>) ?? t.error)`. Change nothing else (the abort/epoch guards around it must stay exactly as they are).
