---
id: P-6a
title: WCAG contrast checker for theme tokens
area: photographer
size: S
depends: []
edit:
  - apps/photographer/scripts/check-contrast.mjs (new)
  - apps/photographer/src/theme-contrast.test.ts (new)
context:
  - apps/photographer/src/paper-theme.ts
verify:
  - cd apps/photographer && node scripts/check-contrast.mjs
  - cd apps/photographer && npx vitest run src/theme-contrast.test.ts
---
## Goal
Measure, not eyeball: compute contrast for the foreground/background colour pairs of both themes defined in `paper-theme.ts` (`photographerDarkTheme`, `photographerLightTheme`).

## Spec
`scripts/check-contrast.mjs`: export `contrastRatio(hexA, hexB)` (WCAG 2.x relative luminance, sRGB linearisation) and `PAIRS` = list of `[themeKey, foregroundColorKey, backgroundColorKey, minRatio]` such as `onSurface/surface 4.5`, `onBackground/background 4.5`, `onPrimary/primary 4.5`, `onSurfaceVariant/surfaceVariant 4.5`, `error/background 3`, `outline/background 3` (use only color keys that exist in the shown theme file; skip missing ones with a warning). Because `paper-theme.ts` is TypeScript, parse the colors with a regex over the file text (`'#rrggbb'` literals next to their key) instead of importing it. When run as a script print a table and exit 1 if any pair is below its minimum. `theme-contrast.test.ts` imports `contrastRatio` and checks: black/white = 21, same color = 1, `#777777` on white ~4.48 (just below 4.5), and that the script's pair check passes for the current themes (call the exported `evaluate()` function that returns the failing pairs; expect `[]`).
If current themes legitimately fail, DO NOT change colours here: make the test print the failing pairs and keep it failing; colour fixes are a separate task.
