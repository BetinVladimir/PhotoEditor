---
id: X-8a
title: Runbook - worker-unavailable
area: ops
size: XS
depends: []
edit:
  - docs/ops/runbooks/worker-unavailable.md (new)
context:
  - docs/review/MVP/05-deploy-and-ops.md:56-62
verify:
  - grep -q "## Symptoms" docs/ops/runbooks/worker-unavailable.md
  - grep -q "## Resolution" docs/ops/runbooks/worker-unavailable.md
---
## Goal
Operational runbook for the incident: **A GPU worker is not taking tasks**. Trigger: queue q.worker.* grows or WorkerSilent alert.

## Spec
English markdown, <= 70 lines, concrete commands where the repository already provides them (`photo-worker`, `docker compose`, `rabbitmqctl`/`rabbitmq-diagnostics`, `psql`), no invented tooling. Exact headings: `## Symptoms`, `## Impact`, `## Diagnosis` (numbered steps: photo-worker validate-config; WireGuard handshake age (wg show); certificates expiry; clock skew (chronyc tracking); RabbitMQ consumers list; capacity heartbeat rows), `## Resolution`, `## Verification`, `## Escalation`. Mention the related alert names from `deploy/observability/alerts.yml` where applicable. Mark any step that you cannot derive from the repository as `(confirm with ops)`.
