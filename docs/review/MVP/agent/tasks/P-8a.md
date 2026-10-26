---
id: P-8a
title: Reproducible web build script and wrangler path
area: photographer
size: XS
depends: []
edit:
  - apps/photographer/package.json
  - deploy/framevela/photographer/wrangler.jsonc
context:
  - apps/photographer/package.json
  - deploy/framevela/photographer/wrangler.jsonc
verify:
  - cd apps/photographer && node -e "const p=require('./package.json');if(!p.scripts['build:web'])process.exit(1)"
  - python3 -c "t=open('deploy/framevela/photographer/wrangler.jsonc').read(); assert 'work/photographer-web' not in t and 'apps/photographer/dist' in t"
---
## Spec
- `package.json`: add script `"build:web": "expo export --platform web --output-dir dist"`.
- `wrangler.jsonc`: the assets directory currently points at an untracked `work/...` folder; change it to `../../../apps/photographer/dist` (keep the relative form used by the file, everything else untouched, keep JSONC comments).
