---
id: O-7b
title: provision.py - root and volume paths from environment
area: deploy
size: XS
depends: []
edit:
  - deploy/framevela/provision.py
context:
  - deploy/framevela/provision.py:1-12
verify:
  - python3 -m py_compile deploy/framevela/provision.py
  - grep -n 'PHOTO_ROOT' deploy/framevela/provision.py
---
## Goal
Remove two hard-coded host paths (plan O-7).

## Spec
Line 7: `root=pathlib.Path('/srv/photoeditor')` becomes `root=pathlib.Path(os.environ.get('PHOTO_ROOT','/srv/photoeditor'))`; line 8: the `/mnt/HC_Volume_<id>` path becomes `pathlib.Path(os.environ['PHOTO_PG_VOLUME'])` when the variable is set, otherwise keep the old literal as the default so behaviour does not change. Add `import os` if missing. Nothing else.

## Do not
- Do not change any other line, do not reformat.
