---
id: P-3a
title: Prettier configuration and scripts (no reformatting yet)
area: photographer
size: XS
depends: []
edit:
  - apps/photographer/.prettierrc.json (new)
  - apps/photographer/.prettierignore (new)
  - apps/photographer/package.json
context:
  - apps/photographer/package.json
verify:
  - cd apps/photographer && node -e "const p=require('./package.json');if(!p.scripts.format||!p.scripts['format:check'])process.exit(1);JSON.parse(require('fs').readFileSync('.prettierrc.json','utf8'))"
---
## Spec
- `.prettierrc.json`: `{"printWidth":110,"singleQuote":true,"semi":true,"trailingComma":"all","arrowParens":"avoid"}`.
- `.prettierignore`: `dist`, `node_modules`, `.expo`, `coverage`, `*.json`, `src/ui-message-keys.json`, `expo-env.d.ts`.
- `package.json`: add scripts `"format": "prettier --write app screens src"` and `"format:check": "prettier --check app screens src"`; add devDependency `"prettier": "3.3.3"`. Keep key order/indentation of the file; do not touch other fields.
- IMPORTANT: do NOT run the formatter in this task. The mass reformat is a separate human-run commit (see the plan P-3).
