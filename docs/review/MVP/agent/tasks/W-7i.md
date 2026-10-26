---
id: W-7i
title: Worker RPC: compose service, secrets and example env
area: deploy
size: S
depends: [W-7h]
edit:
  - deploy/compose.yaml
  - deploy/env/production.env.example
  - docs/ops/worker-control-plane.md (new)
context:
  - deploy/compose.yaml:160-200
verify:
  - python3 -c "import yaml; yaml.safe_load(open('deploy/compose.yaml'))"
  - grep -q "worker-api" deploy/compose.yaml
  - test -s docs/ops/worker-control-plane.md
---
## Goal
Make the control plane deployable on the small servers; the GPU workers reach it over the VPN.
## Spec
- New service `worker-api` modelled on `object-gateway` (same image, `PHOTO_ROLE: worker-api`, same secrets mounts for its certificate, CA and database password; database role `photo_worker`), NOT published to the host by default; add a `profiles: ["worker-api"]` entry so it is opt-in until the migration is rehearsed. Healthcheck: copy the gateway `photo-healthcheck` call with the worker client certificate, URL `https://127.0.0.1:8444/health/ready`.
- `production.env.example`: document `WORKER_API_ADDR`, and on the worker side `PHOTO_WORKER_CONTROL_URL=https://<vpn-address>:8444` (commented out; empty = direct SQL).
- `docs/ops/worker-control-plane.md` (<= 60 lines): what it is, how to switch one worker (env var), how to roll back (unset the variable), the open risks (single server instance = single point of failure until O-3, certificate per worker from W-8).
## Do not
- Do not enable the profile by default. Do not change existing services.
