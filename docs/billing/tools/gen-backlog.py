#!/usr/bin/env python3
"""Генерирует backlog/BACKLOG-*.md: атомарные задачи для ещё не реализованных таблиц, функций и API.
Источник описаний — дословные строки 03, 04, 09. Статус «нет в миграциях» проверяется по backend/db/migrations.
Использование: python3 tools/gen-backlog.py [--check]"""
import re, os, sys, glob, difflib
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, HERE)
from tokens import est
DOCS = os.path.normpath(os.path.join(HERE, "..")); REPO = os.path.normpath(os.path.join(DOCS, "..", ".."))
OUT = os.path.join(DOCS, "backlog"); CHUNK = 3000
def rd(p): return open(p, encoding="utf-8").read()
sql = "".join(rd(f) for f in sorted(glob.glob(os.path.join(REPO, "backend/db/migrations/02[0-9][0-9][0-9]_*.sql"))))
have_t = set(re.findall(r"CREATE TABLE (?:IF NOT EXISTS )?photo\.(\w+)", sql))
have_f = set(re.findall(r"FUNCTION photo\.(\w+)\(", sql))
d3, d4, d9 = (rd(os.path.join(DOCS, n)) for n in ("03-data-model.md", "04-functions-and-api.md", "09-codegen-contract.md"))
def rows(text, pat):
    out = {}
    for ln in text.split("\n"):
        m = re.match(r"^\|\s*(%s)\s*\|(.*)\|\s*$" % pat, ln)
        if m: out.setdefault(m.group(1), []).append(ln.strip())
    return out
t3 = rows(d3, r"[a-z][a-z0-9_]+"); t9 = rows(d9, r"[a-z][a-z0-9_]+")
f4 = rows(d4, r"(?:billing|account|legal|privacy|erasure)_[a-z0-9_]+")
api = [ln.strip() for ln in d4.split("\n") if re.match(r"^\|\s*(GET|POST|PUT|PATCH|DELETE)\s+/", ln)]
tables = sorted(n for n in t3 if re.match(r"(billing|legal|privacy|erasure|tenant_billing|consent)_", n) and n not in have_t and n not in ("billing_class",))
funcs = sorted(n for n in f4 if n not in have_f)
GROUP = {"finance": ("BILL-04", "quotes orders order_lines invoices invoice_allocations payments refunds change_requests customers contacts subscriptions subscription_items custom_assignments custom_limits tenant_billing_profiles".split(),
          "create_quote accept_quote request_change attach_checkout apply_paid_upgrade apply_refund assign_custom ensure_profile create_cost_quote publish_plan_version bind_provider_price".split()),
         "provider": ("BILL-07/08", "webhook_inbox outbox reconciliation_runs notification_marks".split(),
          "record_webhook claim_webhook claim_outbox finish_outbox apply_provider_snapshot".split()),
         "usage": ("BILL-05/06/09", "entitlement_allocations usage_summaries usage_ticket_allocations photo_runs storage_addons storage_objects storage_usage egress_ledger".split(),
          "admit_usage_request allowance_budgets close_lease cover_overrun expire_buckets grant_paid_allocation hold_photo_slot issue_ticket release_ticket settle_ticket register_photo_success project_usage rollover_bucket transfer_rollover_remainder apply_period_boundary reconcile_uncertain_chunk reconcile_storage record_egress_chunk reserve_delivery_chunk open_delivery apply_storage_entitlement".split()),
         "legal": ("BILL-11/12", ["legal_document_versions", "privacy_retention_policies", "privacy_retained_records", "privacy_erasure_tombstones"], [])}
def group_of(name, kind):
    for g, (bill, ts, fs) in GROUP.items():
        key = name.split("_", 1)[1] if "_" in name else name
        if (kind == "t" and (key in ts or name in ts)) or (kind == "f" and (key in fs or name in fs)): return g
    return "legal" if name.startswith(("account_", "privacy_", "legal_", "erasure_")) else "finance"
SCAFFOLD = ("Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): "
 "`contracts/{g}-tables.json`, `contracts/{g}-functions.json`, `contracts/{g}-functions.sql`, `scripts/generate-billing-{g}.py` (копия `scripts/generate-billing-quota.py`), "
 "`tests/contracts/test_billing_{g}_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_{g}.py` (копия структуры `tests/integration/test_billing_quota.py`).")
def similar(n):
    c = difflib.get_close_matches(n, sorted(have_f), n=3, cutoff=0.55); return ", ".join("`%s`" % x for x in c) or "нет"
def table_task(i, g, n):
    r3 = "\n".join(t3.get(n, [])); r9 = "\n".join(l for l in t9.get(n, []) if l not in t3.get(n, []))
    return ["### %s-T%02d — таблица `%s`" % (g[:3].upper(), i, n), "",
     "- Статус: нет `CREATE TABLE photo.%s` в `backend/db/migrations`." % n,
     "- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).",
     "- Выход: запись в `contracts/%s-tables.json`, затем перегенерация миграции генератором этапа." % g,
     "- Критерий: A. `scripts/generate-billing-%s.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет." % g,
     "- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.",
     "- Из 03:", "", r3] + (["- Уточнения из 09:", "", r9] if r9 else []) + [""]
def func_task(i, g, n):
    return ["### %s-F%02d — функция `%s`" % (g[:3].upper(), i, n), "",
     "- Статус: нет `FUNCTION photo.%s(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): %s." % (n, similar(n)),
     "- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.",
     "- Выход: тело в `contracts/%s-functions.sql`, запись в `contracts/%s-functions.json`, перегенерация миграции." % (g, g),
     "- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.",
     "- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.",
     "- Из 04:", "", f4[n][0], ""]
def api_task(i, line):
    cells = [c.strip() for c in line.strip("|").split("|")]
    return ["### API-%02d — `%s`" % (i, cells[0]), "",
     "- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).",
     "- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.",
     "- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.",
     "- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.",
     "- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.",
     "- Из 04:", "", line, ""]
FILES = {}
def emit(g, items):
    chunks, cur = [], []
    for it in items:
        if cur and est("\n".join(cur + it)) > CHUNK: chunks.append(cur); cur = []
        cur = cur + it
    if cur: chunks.append(cur)
    return chunks
idx = ["# Бэклог 1В–4: задачи для ещё не реализованного", "",
       "Сгенерировано `tools/gen-backlog.py` из строк 03/04/09 и сверки с `backend/db/migrations`. Вручную не править.",
       "ВНИМАНИЕ: названия функций в 04 — целевые. Этапы 1Б реализовали часть из них под другими именами (см. 12–18 и `contracts/*-functions.json`). Перед каждой задачей `*-F*` проверить «возможно покрыта»: если функция уже есть, задачу не делать, а записать в `STATUS.md`, чем она покрыта.",
       "Порядок: сначала `*-00` (каркас группы), затем таблицы, затем функции, затем API. Зависимости — по пакетам BILL в `08-delivery-migrations-e2e.md`.", "",
       "| Группа | Пакеты | Таблиц | Функций | Файлы |", "|---|---|---|---|---|"]
for g, (bill, _, _) in GROUP.items():
    ts = [n for n in tables if group_of(n, "t") == g]; fs = [n for n in funcs if group_of(n, "f") == g]
    items = [["### %s-00 — каркас этапа группы `%s`" % (g[:3].upper(), g), "", "- Выход: " + SCAFFOLD.format(g=g), "- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.", "- Запреты: R9, R28.", ""]]
    items += [table_task(i, g, n) for i, n in enumerate(ts, 1)] + [func_task(i, g, n) for i, n in enumerate(fs, 1)]
    names = []
    for k, ch in enumerate(emit(g, items), 1):
        nm = "BACKLOG-%s-%d.md" % (g, k); names.append(nm)
        FILES[nm] = "\n".join(["# Бэклог: группа %s (%s), часть %d" % (g, bill, k), ""] + ch)
    idx.append("| %s | %s | %d | %d | %s |" % (g, bill, len(ts), len(fs), ", ".join("[%s](%s)" % (x, x) for x in names)))
apis = [api_task(i, l) for i, l in enumerate(api, 1)]
names = []
for k, ch in enumerate(emit("api", apis), 1):
    nm = "BACKLOG-api-%d.md" % k; names.append(nm); FILES[nm] = "\n".join(["# Бэклог: API (BILL-10), часть %d" % k, ""] + ch)
idx.append("| api | BILL-10 | — | %d операций | %s |" % (len(api), ", ".join("[%s](%s)" % (x, x) for x in names)))
import backlog_extra
xf, xi, xc = backlog_extra.build(DOCS, est, CHUNK)
FILES.update(xf)
for g, bill, _, cnt, nms in xi: idx.append("| %s | %s | — | %s | %s |" % (g, bill, cnt, ", ".join("[%s](%s)" % (x, x) for x in nms)))
idx += ["", "Stripe-задачи не требуют ключей: проверяются на fake-провайдере (`STR-01`); `STR-14` и `STR-15` ждут ключей (`OPEN-14`, порядок — `STRIPE-CONFIG.md`).", "e2e: каждый сценарий — один положительный и один отрицательный тест; сценарии зависят от остальных групп."]
FILES["README.md"] = "\n".join(idx) + "\n"
check = "--check" in sys.argv; bad = 0; os.makedirs(OUT, exist_ok=True)
for nm, tx in FILES.items():
    p = os.path.join(OUT, nm)
    if check:
        if not os.path.exists(p) or rd(p) != tx: print("drift:", nm); bad += 1
    else: open(p, "w", encoding="utf-8").write(tx)
if not check: print("таблиц:", len(tables), "функций:", len(funcs), "API:", len(api), "stripe/ui/e2e:", xc, "файлов:", len(FILES))
sys.exit(1 if bad else 0)
