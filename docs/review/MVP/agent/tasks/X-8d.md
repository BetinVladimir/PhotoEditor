---
id: X-8d
title: Runbook - broker-failure
area: ops
size: XS
depends: []
edit:
  - docs/ops/runbooks/broker-failure.md (new)
context:
  - docs/review/MVP/05-deploy-and-ops.md:56-62
verify:
  - grep -q "## Symptoms" docs/ops/runbooks/broker-failure.md
  - grep -q "## Resolution" docs/ops/runbooks/broker-failure.md
---
## Goal
Operational runbook for the incident: **RabbitMQ is down or partitioned**. Trigger: workers report disconnects; API outbox lag grows.

## Spec
English markdown, <= 70 lines, concrete commands where the repository already provides them (`photo-worker`, `docker compose`, `rabbitmqctl`/`rabbitmq-diagnostics`, `psql`), no invented tooling. Exact headings: `## Symptoms`, `## Impact`, `## Diagnosis` (numbered steps: what is safe (tasks keep their leases in PostgreSQL), restart order (broker, publisher, workers), verifying quorum queues, checking outbox drained, confirming no duplicate commits), `## Resolution`, `## Verification`, `## Escalation`. Mention the related alert names from `deploy/observability/alerts.yml` where applicable. Mark any step that you cannot derive from the repository as `(confirm with ops)`.
