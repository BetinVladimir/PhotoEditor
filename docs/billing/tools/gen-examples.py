#!/usr/bin/env python3
"""Генерирует EXAMPLES-GO.md, EXAMPLES-API.md, EXAMPLES-TESTS.md из живых исходников репозитория.
Образцы не пишутся вручную: --check падает, если исходник изменился (образец устарел).
Использование: python3 tools/gen-examples.py [--check]  (запускать из любого каталога)"""
import json, os, re, sys
DOCS = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
REPO = os.path.normpath(os.path.join(DOCS, "..", ".."))
def rd(p): return open(os.path.join(REPO, p), encoding="utf-8").read()
def go_func(path, name):
    t = rd(path); m = re.search(r"^func %s\(.*?^}\n" % re.escape(name), t, re.S | re.M)
    if not m: sys.exit("нет func %s в %s" % (name, path))
    return m.group(0).rstrip()
def py_func(path, name):
    t = rd(path); m = re.search(r"^def %s\(.*?(?=^\S|\Z)" % re.escape(name), t, re.S | re.M)
    if not m: sys.exit("нет def %s в %s" % (name, path))
    return m.group(0).rstrip()
def lines(path, a, b): return "\n".join(rd(path).split("\n")[a - 1:b])
def block(src, lang, body): return "### Источник: `%s`\n\n```%s\n%s\n```\n" % (src, lang, body)

FILES = {}
# ---------- Go ----------
W = "backend/internal/billing/workflow.go"
FILES["EXAMPLES-GO.md"] = "\n".join([
 "# EXAMPLES-GO — образцы Go-кода биллинга", "",
 "Сгенерировано `tools/gen-examples.py` из живых файлов. Вручную не править. Копировать форму, не содержимое.", "",
 "## 1. Обёртка вокруг SQL-функции: Prepare с холодным таймаутом, Bind с hash запроса, MapError", "",
 "Правила формы: Prepare берёт блокировки биллинга раньше run/quota locks; холодное ожидание ограничено 200 мс и даёт `BILLING_UNAVAILABLE` 503; Bind принимает `canonical.Hash(request)` как digest; все SQL-ошибки идут через `MapError`.", "",
 block(W + " (PrepareApply)", "go", go_func(W, "PrepareApply")),
 block(W + " (BindApply)", "go", go_func(W, "BindApply")),
 block(W + " (MapError)", "go", go_func(W, "MapError")),
 "## 2. HTTP-handler с Idempotency-Key, интерфейсом зависимости и защитой от поздних ответов", "",
 "Форма: интерфейс сервиса → проверка контекста → route id → `idempotency.ValidKey` → decodeJSON → Validate → вызов сервиса → проверка контекста → writeError/writeJSON.", "",
 block("backend/internal/httpapi/delivery_download_intent.go", "go", lines("backend/internal/httpapi/delivery_download_intent.go", 13, 55)),
 "Регистрация маршрута: см. `backend/internal/httpapi/catalog.go` (блок `r.Post(\"/api/v1/tenants/{t}/legacy-imports/reconcile\"` — `s.tenantRequest`, `decodeJSON`, `writeJSON`).", ""])
# ---------- API ----------
ep2 = json.load(open(os.path.join(REPO, "docs/implementation-v2/contracts/endpoints.json")))["endpoints"]
e2 = next(e for e in ep2 if e.get("handler") == "Download.CreateIntent"); e2 = {k: v for k, v in e2.items() if k != "description"}
ep1 = json.load(open(os.path.join(REPO, "docs/implementation/contracts/endpoints.json")))["endpoints"]
e1 = next((e for e in ep1 if e.get("idempotency") and e.get("status") == 201), next(e for e in ep1 if e.get("idempotency"))); e1 = {k: v for k, v in e1.items() if k != "description"}
sc = json.load(open(os.path.join(REPO, "docs/implementation-v2/contracts/schemas.json")))["$defs"]
dto = {k: sc[k] for k in ("OwnerDownloadIntentRequest", "OwnerDownloadIntent")}
FILES["EXAMPLES-API.md"] = "\n".join([
 "# EXAMPLES-API — образцы endpoint и DTO", "",
 "Сгенерировано `tools/gen-examples.py`. Вручную не править.", "",
 "Правила (09 §1, §7): новые идентичности добавлять в ОБА файла endpoints.json (`docs/implementation/contracts/` и `docs/implementation-v2/contracts/`); DTO — в `docs/implementation-v2/contracts/schemas.json`, `$defs`; `additionalProperties=false`, явный `required`, запрос не принимает server-only суммы, actor и tenant; tenant — параметр пути `{t}`.", "",
 "### Источник: `docs/implementation-v2/contracts/endpoints.json` (формат v2)", "", "```json", json.dumps(e2, ensure_ascii=False, indent=2), "```", "",
 "### Источник: `docs/implementation/contracts/endpoints.json` (формат v1: другие имена полей)", "", "```json", json.dumps(e1, ensure_ascii=False, indent=2), "```", "",
 "### Источник: `docs/implementation-v2/contracts/schemas.json`, `$defs`", "", "```json", json.dumps(dto, ensure_ascii=False, indent=2), "```", ""])
# ---------- Tests ----------
GT = "backend/internal/billing/operations_integration_test.go"
PT = "tests/integration/test_billing_routed_apply.py"
FILES["EXAMPLES-TESTS.md"] = "\n".join([
 "# EXAMPLES-TESTS — образцы тестов", "",
 "Сгенерировано `tools/gen-examples.py`. Вручную не править.", "",
 "## 1. Python integration-тест SQL-функции (PostgreSQL, миграции до нужного head)", "",
 "Форма: `pytestmark=pytest.mark.migration_head(N)` фиксирует head миграций; фикстура `db` — одноразовая БД; проверяется точный повтор, конфликт по другому hash и отсутствие второго списания.", "",
 block(PT + " (шапка)", "python", lines(PT, 1, 12)),
 block(PT + " (test_routed_binding_replay_and_conflict)", "python", py_func(PT, "test_routed_binding_replay_and_conflict")),
 "## 2. Go integration-harness под ролью photo_scheduler/photo_api", "",
 "Форма: тест запускается из Python с фикстурой в env; без неё `t.Skip`. Это значит: `go test` без Python-обвязки НЕ проверяет эти сценарии — не считать их пройденными.", "",
 block(GT + " (начало)", "go", lines(GT, 15, 62)), ""])

check = "--check" in sys.argv; bad = 0
for name, text in FILES.items():
    p = os.path.join(DOCS, name)
    if check:
        if not os.path.exists(p) or open(p, encoding="utf-8").read() != text: print("drift:", name); bad += 1
    else: open(p, "w", encoding="utf-8").write(text)
sys.exit(1 if bad else 0)
