---
id: T-5a
title: Coverage targets (Go, worker, portal)
area: tests
size: S
depends: []
edit:
  - Makefile
  - scripts/check-coverage.py (new)
  - scripts/test_check_coverage.py (new)
context:
  - Makefile:1-12
verify:
  - python3 -m pytest scripts/test_check_coverage.py -q -p no:cacheprovider
  - grep -q "^coverage:" Makefile
---
## Spec
1. `scripts/check-coverage.py go-func <file> <threshold> [--package-prefix a,b]`: parses the output of `go tool cover -func` (lines `path/file.go:LINE:\tfunc\tPCT%`, last line `total:\t(statements)\tPCT%`). Without prefix check the `total`; with prefixes compute the mean of per-function percentages for lines whose path contains one of the prefixes. Exit 1 when below the threshold; print the number. Pure function `parse_go_func(text) -> list[(path, pct)]` for tests.
2. `Makefile`: new target (add to `.PHONY`)
```
coverage:
	cd backend && go test -coverprofile=/tmp/photo-cover.out ./internal/... >/dev/null
	cd backend && go tool cover -func=/tmp/photo-cover.out > /tmp/photo-cover.txt
	python3 scripts/check-coverage.py go-func /tmp/photo-cover.txt 70 --package-prefix internal/auth,internal/tenant,internal/gateway,internal/portal
	$(RUN) $(PYTHON) -m pytest workers/tests --cov=photo_worker.runtime --cov-fail-under=75 -q -p no:cacheprovider
	cd apps/portal && npx vitest run --coverage
```
(Use TAB indentation; keep the existing targets intact. The worker/portal lines require `pytest-cov` and `@vitest/coverage-v8` to be installed - mention this in a Makefile comment above the target.)
3. Tests for `parse_go_func` with a 4-line sample, and for the threshold decision.
