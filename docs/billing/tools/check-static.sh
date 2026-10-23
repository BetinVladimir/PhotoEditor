#!/usr/bin/env bash
# Статические проверки биллинга: без PostgreSQL и Go. Выход 0 только если всё прошло.
# Запуск из любого каталога: bash docs/billing/tools/check-static.sh
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; DOCS="$(cd "$HERE/.." && pwd)"; REPO="$(cd "$DOCS/../.." && pwd)"
PY="${PYTHON:-python3}"; fail=0
run() { local name="$1"; shift; if "$@" >/tmp/check-static.out 2>&1; then echo "PASS  $name"; else echo "FAIL  $name"; head -20 /tmp/check-static.out | sed 's/^/        /'; fail=1; fi; }
cd "$REPO"
run "lint-docs"            "$PY" "$DOCS/tools/lint-docs.py"
run "gen-briefs --check"   "$PY" "$DOCS/tools/gen-briefs.py" --check
run "gen-examples --check" "$PY" "$DOCS/tools/gen-examples.py" --check
run "gen-backlog --check"  "$PY" "$DOCS/tools/gen-backlog.py" --check
run "gen-error-codes --check" "$PY" "$DOCS/tools/gen-error-codes.py" --check
for g in catalog quota workflow routed-workflow render editor operations paid-operation selected; do
  run "generate-billing-$g --check" "$PY" "scripts/generate-billing-$g.py" --check
done
if "$PY" -c "import pytest" 2>/dev/null; then
  run "contract tests (tests/contracts/test_billing_*.py)" "$PY" -m pytest -q -p no:cacheprovider tests/contracts/test_billing_*.py
else echo "SKIP  contract tests: нет pytest (pip install pytest)"; fi
echo "НЕ покрыто этим скриптом: tests/integration (нужен PostgreSQL) и go test (нужен Go)."
exit $fail
