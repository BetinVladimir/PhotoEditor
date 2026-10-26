---
id: P-1d
title: Hide the style-training button in the MVP profile
area: photographer
size: XS
depends: [P-1b]
edit:
  - apps/photographer/screens/styles/[id]/index.tsx
context:
  - apps/photographer/src/mvp-profile.ts
verify:
  - cd apps/photographer && npx tsc --noEmit
---
## Spec
Edit by exact substring (the file has very long lines). Replace
`<Button  disabled={!canEdit} onPress={()=>router.push({pathname:"/styles/[id]/training",params:{id,tenant}})} mode="contained" contentStyle={{minHeight:48}}>{t('catalog_style_train')}</Button>`
by
`{isRouteEnabled("/styles/[id]/training")?<Button  disabled={!canEdit} onPress={()=>router.push({pathname:"/styles/[id]/training",params:{id,tenant}})} mode="contained" contentStyle={{minHeight:48}}>{t('catalog_style_train')}</Button>:null}`
and add the import by replacing the first occurrence of the exact text `export default function` with `import {isRouteEnabled} from '../../../src/mvp-profile';\nexport default function`. If `export default function` does not occur exactly once, answer `BLOCKED: need file head`.
