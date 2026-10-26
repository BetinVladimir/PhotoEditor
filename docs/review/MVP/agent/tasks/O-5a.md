---
id: O-5a
title: Backup/restore runbook, pgBackRest example, drill template
area: ops
size: S
depends: []
edit:
  - deploy/pg/pgbackrest.conf.example (new)
  - docs/ops/backup-restore.md (new)
  - docs/ops/restore-drills/TEMPLATE.md (new)
context:
  - deploy/BACKUP_OPERATOR.md:1-60
verify:
  - grep -q "repo1-type=s3" deploy/pg/pgbackrest.conf.example
  - grep -q "RPO" docs/ops/backup-restore.md
  - grep -q "Result" docs/ops/restore-drills/TEMPLATE.md
---
## Spec
1. `pgbackrest.conf.example`: stanza `photo`, `pg1-path=/var/lib/postgresql/data`, `repo1-type=s3`, `repo1-s3-bucket=<bucket>`, `repo1-s3-endpoint`, `repo1-s3-region`, `repo1-retention-full=4`, `repo1-retention-diff=7`, `compress-type=zst`, `archive-async=y`, `repo1-cipher-type=aes-256-cbc` (key from secret file placeholder). Comments only; no real values.
2. `docs/ops/backup-restore.md` (English, <= 90 lines): sections `## Targets` (RPO <= 15 min, RTO <= 4 h for the closed beta), `## What is backed up` (PostgreSQL WAL + daily full/diff; S3 bucket with versioning enabled - restore code uses version ids; RabbitMQ definitions export; secrets directory by the operator's secret manager), `## Daily/Weekly/Monthly` schedule table, `## Restore procedure` (numbered steps: stop writers, restore latest backup to a new volume, replay WAL to target time, start PostgreSQL, run `make test-restore`-equivalent acceptance via `photo-worker backup-accept`, resume), pointing to `deploy/BACKUP_OPERATOR.md` for the operator commands.
3. `TEMPLATE.md`: front matter-free checklist with fields `Date`, `Operator`, `Backup used`, `Target time`, `Start/End`, `Measured RPO`, `Measured RTO`, `Steps that failed`, `Result (pass/fail)`, `Follow-ups`.
