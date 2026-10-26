---
id: X-8c
title: Runbook - queue-growth
area: ops
size: XS
depends: []
edit:
  - docs/ops/runbooks/queue-growth.md (new)
context:
  - docs/review/MVP/05-deploy-and-ops.md:56-62
verify:
  - grep -q "## Symptoms" docs/ops/runbooks/queue-growth.md
  - grep -q "## Resolution" docs/ops/runbooks/queue-growth.md
---
## Goal
Operational runbook for the incident: **Queue depth grows and tasks wait**. Trigger: QueueGrowing alert.

## Spec
English markdown, <= 70 lines, concrete commands where the repository already provides them (`photo-worker`, `docker compose`, `rabbitmqctl`/`rabbitmq-diagnostics`, `psql`), no invented tooling. Exact headings: `## Symptoms`, `## Impact`, `## Diagnosis` (numbered steps: compare worker count and capacity; look for poison messages and dead letters (q.dead-letter); check scheduler leader; check outbox lag; how to scale workers; when to pause intake), `## Resolution`, `## Verification`, `## Escalation`. Mention the related alert names from `deploy/observability/alerts.yml` where applicable. Mark any step that you cannot derive from the repository as `(confirm with ops)`.
