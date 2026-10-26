---
id: PT-4h
title: Portal: extract useGuestUpload from Portal.tsx
area: portal
size: S
depends: [PT-4g]
edit:
  - apps/portal/src/components/Portal.tsx
  - apps/portal/src/features/upload/useGuestUpload.ts (new)
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
Move uploadRecoverably and lib/upload-recovery.ts handling out of `Portal.tsx` into the hook `useGuestUpload` in `apps/portal/src/features/upload/useGuestUpload.ts`. `Portal.tsx` calls the hook. Behaviour must not change: `Portal.test.tsx` and all `lib/*.test.ts` stay green and unmodified.

## Spec
- Take the exact list of state variables, refs and functions for this hook from `notes/PT-4-state-map.md`, section "Proposed hooks" (name `useGuestUpload`). Anything shared with another hook stays in `Portal.tsx` and is passed in as a parameter, as the note says.
- Move code, do not rewrite it: statement order, dependency arrays and the `AbortController` handling are preserved. The hook returns an object with exactly the names `Portal.tsx` uses afterwards.
- If the state map is missing this hook or contradicts the code, answer `BLOCKED: <what>`.

## Do not
- Do not change other hooks' code, `lib/*`, or the JSX apart from the renamed returned values.
