---
id: PT-4i
title: Portal: final size check and import cleanup
area: portal
size: XS
depends: [PT-4h]
edit:
  - apps/portal/src/components/Portal.tsx
context:
  - apps/portal/src/components/Portal.tsx:1-200
verify:
  - cd apps/portal && npx tsc --noEmit
  - cd apps/portal && npx vitest run
  - test $(wc -l < apps/portal/src/components/Portal.tsx) -le 200
---
## Goal
After the hooks are extracted `Portal.tsx` must be composition and markup only (<= 200 lines).

## Steps
1. Run `wc -l apps/portal/src/components/Portal.tsx`. If it is <= 200, only remove unused imports reported by `npx tsc --noEmit --noUnusedLocals`.
2. If it is larger, answer `BLOCKED: Portal.tsx has N lines; remaining logic: <list the top-level functions/state still inside>` - a human will add cards.

## Do not
- Do not move code in this card.
