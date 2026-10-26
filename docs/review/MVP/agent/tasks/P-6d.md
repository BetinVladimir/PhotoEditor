---
id: P-6d
title: Settings: theme mode selector on the profile screen
area: photographer
size: S
depends: [P-6c]
edit:
  - apps/photographer/screens/settings/profile.tsx
context:
  - apps/photographer/screens/settings/profile.tsx
  - apps/photographer/src/theme-mode.ts
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/paper-ui-policy.test.ts
---
## Goal
Let the user choose System / Light / Dark.

## Spec
Add a section to `screens/settings/profile.tsx` using the same react-native-paper components the screen already uses (`SegmentedButtons` or `RadioButton.Group`), bound to `useThemeMode()` from `src/paper-provider.tsx`. Labels come from the locale catalogue the screen already uses: add keys `theme_mode`, `theme_system`, `theme_light`, `theme_dark` to every locale file in the same place as other keys of this screen (find them with `git grep -n "settings_profile" -- apps/photographer`); if there is more than one locale file, add all of them (English text for non-translated locales is acceptable).

## Do not
- Do not add new primitives from `react-native` (the policy test forbids them). Do not touch other screens.
