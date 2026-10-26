---
id: B-8a
title: Audit script for context.Background() and panic( in backend code
area: backend
size: S
depends: []
edit:
  - scripts/audit-context-panic.py (new)
  - scripts/test_audit_context_panic.py (new)
context: []
verify:
  - python3 -m pytest scripts/test_audit_context_panic.py -q -p no:cacheprovider
---
## Goal
Produce the table promised by plan item B-8 without hand work.

## Spec
`scripts/audit-context-panic.py [--root backend] [--out docs/review/MVP/audit-context-panic.md]`:
- walk `*.go` under `backend/internal` and `backend/cmd`, skipping `_test.go`, `.cache`, `_work`, vendor dirs;
- report every line containing `context.Background()` or `context.TODO()` or a `panic(` call (skip occurrences inside `//` comments and string literals);
- classify by simple rules: path contains `/cmd/` or function name starts with `main`/`run` -> `process-root`; inside `func (...) ServeHTTP`/ handler closures (`http.ResponseWriter` in the enclosing function signature within 60 lines above) -> `request-scope: should use r.Context()`; `panic(` in `init()` or `must*` helper -> `startup`; everything else `review`;
- output markdown: summary counts per class, then a table `file:line | kind | class | line text (<=100 chars)`; exit code 0 always (informational), `--fail-on request-scope` makes it exit 1 when any exist.
- pure stdlib. Write a function `scan_text(path, text)` returning findings so it can be unit tested.

## Test (`scripts/test_audit_context_panic.py`)
Use `importlib.util.spec_from_file_location` to import the script (file name has dashes). Cases: detects Background in code, ignores in comment and in string, classifies a handler closure as request-scope, panic in `func init()` as startup.
