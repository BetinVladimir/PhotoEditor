---
id: B-3b
title: Baseline equivalence test (skipped without a database)
area: backend
size: S
depends: [B-3a]
edit:
  - tests/migration/test_baseline_equivalence.py (new)
context:
  - scripts/build_baseline_lib.py
verify:
  - python3 -m pytest tests/migration/test_baseline_equivalence.py -q -p no:cacheprovider -rs
---
## Goal
The acceptance test for B-3, ready for a human to run against two empty PostgreSQL databases.

## Spec
- Skip unless `PHOTO_TEST_PG_ADMIN_URL` is set (and `pg_dump` is on PATH).
- Test `test_baseline_plus_tail_equals_full_chain`: create DB A, apply the full goose chain (`backend/db/migrations`, command given by `PHOTO_TEST_MIGRATE_FULL`, an env variable holding the shell command with `{url}` placeholder); create DB B, apply baseline (`PHOTO_TEST_MIGRATE_BASELINE`, same convention); dump both with `pg_dump --schema-only --no-comments`; `assert diff_dumps(a, b) == []` printing the first 80 diff lines on failure. Also compare `SELECT proname, prosecdef, proconfig, pg_get_userbyid(proowner) FROM pg_proc JOIN pg_namespace ...` for schema `photo`.
- Always drop the two databases in `finally`.

## Do not
- Do not embed connection strings or passwords. Do not run migrations from a Python import.
