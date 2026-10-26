---
id: PT-4c
title: Portal: extract usePortalSession from Portal.tsx
area: portal
size: S
depends: [PT-4b]
edit:
  - apps/portal/src/components/Portal.tsx
  - apps/portal/src/features/session/usePortalSession.ts (new)
context:
  - apps/portal/src/components/Portal.tsx:1-180
  - apps/portal/src/components/Portal.tsx:181-355
  - docs/review/MVP/agent/notes/PT-4-state-map.md
verify:
  - cd apps/portal && npx tsc --noEmit
  - cd apps/portal && npx vitest run
  - cd apps/portal && npx vitest run src/components/Portal.test.tsx
---
## Goal
Move exchange, csrf, epoch/authority refs, refuseDownloadAccess; the session state machine loading|ready|invalid|error (useReducer) out of `Portal.tsx` into the hook `usePortalSession` in `apps/portal/src/features/session/usePortalSession.ts`. `Portal.tsx` calls the hook. Behaviour must not change: `Portal.test.tsx` and all `lib/*.test.ts` stay green and unmodified.

## Spec
- Take the exact list of state variables, refs and functions for this hook from `notes/PT-4-state-map.md`, section "Proposed hooks" (name `usePortalSession`). Anything shared with another hook stays in `Portal.tsx` and is passed in as a parameter, as the note says.
- Move code, do not rewrite it: statement order, dependency arrays and the `AbortController` handling are preserved. The hook returns an object with exactly the names `Portal.tsx` uses afterwards.
- If the state map is missing this hook or contradicts the code, answer `BLOCKED: <what>`.

## Do not
- Do not change other hooks' code, `lib/*`, or the JSX apart from the renamed returned values.
