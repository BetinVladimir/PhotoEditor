---
id: B-3a
title: Baseline builder: schema-dump normalizer (pure function + tests)
area: backend
size: S
depends: []
edit:
  - scripts/build_baseline_lib.py (new)
  - tests/migration/test_build_baseline_lib.py (new)
context: []
verify:
  - python3 -m pytest tests/migration/test_build_baseline_lib.py -q -p no:cacheprovider
---
## Goal
First, testable half of plan item B-3: normalise the text of `pg_dump --schema-only` so that two dumps of equivalent schemas compare equal. No database is needed.

## Spec (`scripts/build_baseline_lib.py`, stdlib only)
- `normalize_dump(text: str) -> str`: drop comment lines (`--`), `SET ...;`, `SELECT pg_catalog.set_config(...);`, `\\restrict` / `\\unrestrict` lines, blank-line runs; remove `OWNER TO` clauses only when `keep_owners=False` (default True keeps them: owners matter for `photo_fn_owner`); collapse whitespace inside statements; split into statements on `;` at end of line outside `$$ ... $$` / `$tag$ ... $tag$` bodies and sort the statements of each kind (`CREATE TABLE`, `CREATE INDEX`, `ALTER TABLE ... ADD CONSTRAINT`, `GRANT`, `CREATE FUNCTION`, ...) alphabetically so that creation order does not matter; function bodies are kept verbatim (only trailing whitespace stripped).
- `diff_dumps(a: str, b: str) -> list[str]`: unified diff lines of the normalised statements (empty when equal).
- `statement_kinds(text) -> dict[str,int]` counts per kind (used by humans to sanity-check).
Tests with small inline dumps: reorder of tables is equal; a changed default is a diff; a `$$` body containing `;` is not split; comments/SET ignored; owner kept by default; `GRANT` order irrelevant.

## Do not
- Do not call `pg_dump`, `psql` or any network service in this card.
