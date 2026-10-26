---
id: P-6f-src-read-resource-state
title: Theme tokens in src/read-resource-state.tsx
area: photographer
size: XS
depends: [P-6b, P-6c]
edit:
  - apps/photographer/src/read-resource-state.tsx
context:
  - apps/photographer/src/read-resource-state.tsx
  - docs/review/MVP/agent/notes/P-6-color-map.md
  - apps/photographer/src/paper-theme.ts
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/paper-ui-policy.test.ts
---
## Goal
Replace every hard-coded colour literal in `apps/photographer/src/read-resource-state.tsx` by the theme token that `notes/P-6-color-map.md` assigns to it (rows whose files column lists this file). Literals marked `KEEP` stay unchanged.

## Spec
- Obtain the theme with `useTheme()` from `react-native-paper` inside the component (or pass `colors` to helper functions defined in this file); style objects built at module level with a literal move into a function/`useMemo` of `colors`.
- Run `git grep -nE "#[0-9a-fA-F]{3,8}[^0-9a-zA-Z]|rgba?[(]" -- apps/photographer/src/read-resource-state.tsx` before and after: after the change only `KEEP` literals may remain.

## Do not
- Do not change layout, spacing, text or logic. Do not edit other files.
