---
id: B-4b
title: Enable PHOTO_PROFILE=mvp and TRUSTED_PROXY_CIDRS in compose/env examples
area: deploy
size: XS
depends: [B-4a]
edit:
  - deploy/compose.yaml
  - deploy/env/production.env.example
context:
  - deploy/compose.yaml:14-28
  - deploy/env/production.env.example
verify:
  - python3 -c "import yaml; yaml.safe_load(open('deploy/compose.yaml'))"
  - grep -q "PHOTO_PROFILE" deploy/compose.yaml
  - grep -q "TRUSTED_PROXY_CIDRS" deploy/env/production.env.example
---
## Spec
- In the shared service environment block of `compose.yaml` (the anchor that already contains `APP_ORIGIN`, `PORTAL_ORIGIN` and `TRUSTED_PROXY_CIDRS`) add `PHOTO_PROFILE: ${PHOTO_PROFILE:-mvp}`.
- In `production.env.example` document both variables with comments: `TRUSTED_PROXY_CIDRS=172.18.0.0/16` (the compose `backend`/proxy network; must be the subnet from which Caddy reaches the API; leave empty = rate limits are shared by all clients) and `PHOTO_PROFILE=mvp`. Do not change any other line.
