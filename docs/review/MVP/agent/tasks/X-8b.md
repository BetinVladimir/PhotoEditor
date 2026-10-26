---
id: X-8b
title: Runbook - scratch-disk-full
area: ops
size: XS
depends: []
edit:
  - docs/ops/runbooks/scratch-disk-full.md (new)
context:
  - docs/review/MVP/05-deploy-and-ops.md:56-62
verify:
  - grep -q "## Symptoms" docs/ops/runbooks/scratch-disk-full.md
  - grep -q "## Resolution" docs/ops/runbooks/scratch-disk-full.md
---
## Goal
Operational runbook for the incident: **Worker scratch volume is above 85 percent**. Trigger: DiskScratchHigh alert or DISK_QUOTA task failures.

## Spec
English markdown, <= 70 lines, concrete commands where the repository already provides them (`photo-worker`, `docker compose`, `rabbitmqctl`/`rabbitmq-diagnostics`, `psql`), no invented tooling. Exact headings: `## Symptoms`, `## Impact`, `## Diagnosis` (numbered steps: which task directories are orphaned; safe cleanup rules (never delete a directory of a task with an unexpired lease); how to resize; how to lower WORKER_MAX_SCRATCH_BYTES), `## Resolution`, `## Verification`, `## Escalation`. Mention the related alert names from `deploy/observability/alerts.yml` where applicable. Mark any step that you cannot derive from the repository as `(confirm with ops)`.
