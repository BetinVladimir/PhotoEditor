---
id: T-3a
title: Makefile - lint, typecheck, e2e targets and verify
area: tests
size: XS
depends: [T-1a]
edit:
  - Makefile
context:
  - Makefile:1-8
  - Makefile:48-70
verify:
  - make -n lint typecheck e2e >/dev/null
---
## Spec
Add targets (TAB indented, add to `.PHONY`; keep existing `check-skips` and `verify` but extend `verify`):
```
lint:
	cd backend && go vet ./...
	cd apps/photographer && npm run format:check
typecheck:
	cd apps/photographer && npm run typecheck
	cd apps/portal && npx tsc --noEmit
e2e:
	docker compose -f deploy/compose.e2e.yaml up -d --wait
	cd tests/e2e && npx playwright test; rc=$$?; docker compose -f deploy/compose.e2e.yaml down -v; exit $$rc
verify: lint typecheck test test-web check-skips
```
Replace the existing `verify:` line instead of adding a second one. `e2e` stays out of `verify` until the stack file exists.
