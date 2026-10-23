#!/usr/bin/env python3
"""ERROR-CODES.md: коды RAISE EXCEPTION биллинг-SQL и их обработка в billing.MapError (Go).
Использование: python3 tools/gen-error-codes.py [--check]"""
import re, os, sys, glob
DOCS = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")); REPO = os.path.normpath(os.path.join(DOCS, "..", ".."))
rd = lambda p: open(p, encoding="utf-8").read()
sql = "".join(rd(f) for f in sorted(glob.glob(os.path.join(REPO, "backend/db/migrations/02[56][0-9][0-9]_*billing*.sql"))))
codes = sorted(set(re.findall(r"RAISE EXCEPTION '((?:BILLING|PHOTO_INITIAL)[A-Z0-9_]*)'", sql)))
go = rd(os.path.join(REPO, "backend/internal/billing/workflow.go")); body = go[go.index("func MapError"):]
mapped = {}
for m in re.finditer(r'case ([^:]+):\s*return domain\.NewError\("([A-Z_]+)", (\d+)\)', body):
    for c in re.findall(r'"([A-Z_]+)"', m.group(1)): mapped[c] = (m.group(2), m.group(3))
import json
dec = json.load(open(os.path.join(DOCS, "contracts", "error-classes.json"), encoding="utf-8"))["decisions"]
und = [c for c in codes if c not in mapped and c not in dec]
stale = [c for c in dec if c not in codes]
todo = [c for c in codes if c in dec and dec[c]["class"] == "http"]
L = ["# ERROR-CODES — коды ошибок биллинг-SQL и их HTTP-обработка", "",
     "Сгенерировано `tools/gen-error-codes.py` из миграций 02598–02609, `backend/internal/billing/workflow.go` и решений в `contracts/error-classes.json`. Вручную не править.", "",
     "Решения владельца 23.10.2026 (`OPEN-11`): A — остаётся `500 INTERNAL_ERROR`; B — `422 VALIDATION_FAILED`; C — `409 BILLING_STATE_CONFLICT` (новый код); D — `503 BILLING_UNAVAILABLE`.", "",
     "Всего кодов: %d. Обработано в Go сейчас: %d. Решено, но Go ещё не изменён: %d. Внутренние (500 по решению): %d. Без решения: %d. Устаревших записей в решениях: %d." % (
        len(codes), sum(c in mapped for c in codes), len(todo), sum(1 for c in codes if c in dec and dec[c]["class"] == "internal"), len(und), len(stale)), "",
     "## Обработаны в Go сейчас", "", "| SQL-код | Ответ клиенту | HTTP |", "|---|---|---|"]
L += ["| %s | %s | %s |" % (c, *mapped[c]) for c in codes if c in mapped]
L += ["", "## Решено, нужна правка Go (задача T-02)", "", "| SQL-код | Группа | Ответ клиенту | HTTP |", "|---|---|---|---|"]
L += ["| %s | %s | %s | %s |" % (c, dec[c]["group"], dec[c]["code"], dec[c]["status"]) for c in todo]
L += ["", "## Внутренние: остаются 500 (группа A)", ""] + ["- %s" % c for c in codes if c in dec and dec[c]["class"] == "internal"]
if und: L += ["", "## БЕЗ РЕШЕНИЯ (добавить в error-classes.json)", ""] + ["- %s" % c for c in und]
if stale: L += ["", "## УСТАРЕЛО (нет в SQL, удалить из error-classes.json)", ""] + ["- %s" % c for c in stale]
text = "\n".join(L) + "\n"; p = os.path.join(DOCS, "ERROR-CODES.md")
if "--check" in sys.argv: sys.exit(0 if os.path.exists(p) and rd(p) == text else (print("drift: ERROR-CODES.md") or 1))
open(p, "w", encoding="utf-8").write(text)
