---
id: PT-4a
title: Analysis - state map of Portal.tsx (input for its decomposition)
area: portal
size: S
depends: []
edit:
  - docs/review/MVP/agent/notes/PT-4-state-map.md (new)
context:
  - apps/portal/src/components/Portal.tsx:25-135
  - apps/portal/src/components/Portal.tsx:225-340
verify:
  - test -s docs/review/MVP/agent/notes/PT-4-state-map.md
---
## Goal
Produce a compact, accurate dependency table that a later card (or a human) can use to split the component into hooks without re-reading 330 dense lines. This is a READ-ONLY analysis: output one markdown file.

## Output format (use exactly these sections, <= 120 lines total)
1. `## State and refs`: table `name | kind (useState/useRef/useMemo) | written by (functions) | read by (functions / JSX)` for every state variable and ref in lines 25-112 (the `const [..]` and `useRef` declarations).
2. `## Functions`: table `function | purpose (<= 12 words) | state it reads | state it writes | AbortController used`.
3. `## Proposed hooks`: for each of `usePortalSession`, `useGallery`, `useMediaLinks`, `useSelection`, `useDownloadJob`, `useGuestUpload` list which state variables and functions move into it and what it must return. A variable used by two hooks stays in the parent and is passed down - say so explicitly.
4. `## Risks`: list every place where ordering/`epoch`/`authority` refs make extraction unsafe.
Do not invent names that do not appear in the code; if a name is not visible in the shown ranges write `(not visible)`.
