---
id: P-1c
title: Hide the interchange-import button on the project screen in the MVP profile
area: photographer
size: XS
depends: [P-1b]
edit:
  - apps/photographer/screens/projects/[id]/index.tsx
context:
  - apps/photographer/src/mvp-profile.ts
verify:
  - cd apps/photographer && npx tsc --noEmit
---
## Spec
The file has very long lines, so edit by exact substrings only (the search text may be a part of a line).
1. Replace the exact substring
`<Button mode="outlined" accessibilityRole="button" disabled={!canEdit||busy} onPress={()=>router.push({pathname:"/interchange/import-format",params:{tenant,project:id}})}>{t("interchange_archive_title")}</Button>`
by
`{isRouteEnabled("/interchange/import-format")?<Button mode="outlined" accessibilityRole="button" disabled={!canEdit||busy} onPress={()=>router.push({pathname:"/interchange/import-format",params:{tenant,project:id}})}>{t("interchange_archive_title")}</Button>:null}`
2. Add the import by replacing the first occurrence of the exact text `export default function` with `import {isRouteEnabled} from '../../../src/mvp-profile';\nexport default function`. If that text does not occur exactly once in the file, answer `BLOCKED: need file head`.
