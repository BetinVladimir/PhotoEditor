---
id: P-1e
title: Hide the editor "export interchange bundle" button in the MVP profile
area: photographer
size: XS
depends: [P-1b]
edit:
  - apps/photographer/screens/editor/[version].tsx
context:
  - apps/photographer/src/mvp-profile.ts
verify:
  - cd apps/photographer && npx tsc --noEmit
---
## Spec
Edit by exact substring. Replace
`<Button accessibilityRole="button" disabled={busy||!!conflict||!!layerAttempt.current} onPress={prepareInterchangeExport}    mode="contained" contentStyle={{minHeight:48}}>{t("interchange_editor_export_bundle")}</Button>`
by the same element wrapped as `{isRouteEnabled("/interchange/prepare")?<Button ...same attributes...>{t("interchange_editor_export_bundle")}</Button>:null}`.
Add the import: replace the first occurrence of the exact text `export default function` with `import {isRouteEnabled} from '../../src/mvp-profile';\nexport default function` (path is two levels up from `screens/editor`). If `export default function` does not occur exactly once, answer `BLOCKED: need file head`.
