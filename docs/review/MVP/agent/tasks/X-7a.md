---
id: X-7a
title: Audit SECURITY DEFINER functions (search_path and owner) in migrations
area: ops
size: S
depends: []
edit:
  - scripts/audit-secdef.py (new)
  - scripts/test_audit_secdef.py (new)
context:
  - scripts/check-sql-function-syntax.py:1-60
verify:
  - python3 -m pytest scripts/test_audit_secdef.py -q -p no:cacheprovider
---
## Goal
Hundreds of `SECURITY DEFINER` functions are the real security perimeter. A function without a pinned `search_path` can be hijacked. Detect that statically from the migration text.

## Spec
`scripts/audit-secdef.py [--migrations backend/db/migrations] [--baseline scripts/secdef-baseline.txt] [--update]`:
- parse every `*.sql`; find `CREATE [OR REPLACE] FUNCTION <name>(...) ... SECURITY DEFINER ... ;` statements (a function body is delimited by `$tag$ ... $tag$`; header options can appear before or after the body - search the whole statement text, i.e. from `CREATE` to the closing `$tag$` plus the following option lines up to the next `;`);
- a function is OK when its statement contains `SET search_path` (case-insensitive); later `ALTER FUNCTION ... SET search_path` for the same name also counts (collect those first);
- the LAST definition of a name (by file order) is the effective one;
- print `FAIL name file` for effective definitions without search_path; summary counts; exit 1 if any FAIL not listed in the baseline file (one function name per line); `--update` rewrites the baseline.
Use only the stdlib; handle the multi-megabyte inputs by reading file by file. Function: `analyze(sql_texts: list[tuple[str,str]]) -> dict` (list of `(filename, text)` in order) so it can be tested.

## Test
Three tiny SQL strings: function with search_path (ok); without (fail); without but fixed by a later `ALTER FUNCTION ... SET search_path = pg_catalog, photo` (ok); redefinition where the last one lacks it (fail); a `SECURITY INVOKER` function is ignored.
