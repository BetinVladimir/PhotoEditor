---
id: P-6c
title: Theme mode: system / light / dark with a persisted choice
area: photographer
size: S
depends: []
edit:
  - apps/photographer/src/theme-mode.ts (new)
  - apps/photographer/src/theme-mode.test.ts (new)
  - apps/photographer/src/paper-provider.tsx
context:
  - apps/photographer/src/paper-provider.tsx
  - apps/photographer/src/paper-theme.ts
verify:
  - cd apps/photographer && npx tsc --noEmit
  - cd apps/photographer && npx vitest run src/theme-mode.test.ts
---
## Goal
`photographerLightTheme` exists but nothing selects it. Add the selection logic.

## Spec
- `src/theme-mode.ts`: `export type ThemeMode='system'|'light'|'dark'`; `resolveTheme(mode, systemScheme: 'light'|'dark'|null|undefined): 'light'|'dark'` (system + null -> 'dark', the current look); `parseThemeMode(raw: unknown): ThemeMode` (anything invalid -> 'system'); storage helpers `readThemeMode(storage)` / `writeThemeMode(storage, mode)` over an `{getItem,setItem}` interface with key `photo.theme-mode.v1`.
- `paper-provider.tsx`: `PhotographerPaperProvider` reads the stored mode (async, default 'system' until loaded), uses `useColorScheme()` from react-native, picks the theme with `resolveTheme`, and exposes `useThemeMode()` returning `{mode, setMode}` through a React context. Reuse the existing structure of the file; do not change how the dark theme is provided today when the mode is 'system' and the device is dark.
- Tests (pure): all combinations of `resolveTheme`, `parseThemeMode` with garbage, read/write round trip with an in-memory storage.

## Do not
- Do not edit screens. Do not change theme colours.
