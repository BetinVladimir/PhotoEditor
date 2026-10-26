---
id: P-6b
title: Analysis - map every hard-coded colour literal to a theme token
area: photographer
size: S
depends: []
edit:
  - docs/review/MVP/agent/notes/P-6-color-map.md (new)
context:
  - apps/photographer/src/paper-theme.ts
verify:
  - test -s docs/review/MVP/agent/notes/P-6-color-map.md
---
## Goal
READ-ONLY analysis that makes the per-file replacement cards (P-6f-*) mechanical. Output ONE markdown file.

## Steps
1. Run: `git grep -nE "#[0-9a-fA-F]{3,8}[^0-9a-zA-Z]|rgba?[(]" -- 'apps/photographer/app' 'apps/photographer/screens' 'apps/photographer/src' ':!*test*' ':!*.json' `. (About 60 hits in 24 files; if the output is truncated, run it per directory.)
2. For each distinct literal write one table row: `literal | files (count) | role guess (background / text / border / overlay / canvas drawing) | token`. `token` is the key of `photographerDarkTheme`/`photographerLightTheme` in `paper-theme.ts` to use (`colors.surface`, `colors.onSurface`, `colors.outline`, ...). Choose by ROLE and by nearest colour in the dark theme.
3. Literals that are drawn on a canvas for image analysis (masks, overlays, perspective lines: `mask-canvas`, `perspective-*`, `compare-*`, `region-loupe`) must NOT follow the theme; mark them `KEEP (image overlay)` with a one-line reason and, when useful, name a constant in a new `src/overlay-colors.ts`.
4. End with `## Unmapped` listing literals that need a human decision.
Do not edit code in this card.
