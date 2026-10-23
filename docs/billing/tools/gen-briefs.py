#!/usr/bin/env python3
"""Генерирует briefs/BRIEF-*.md — короткие самодостаточные карточки этапов из contracts/.
Карточка влезает в малый контекст (цель: до ~4K токенов) и не может разойтись с кодом: всё берётся из JSON/SQL.
Использование: python3 tools/gen-briefs.py [--check]"""
import json, os, re, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tokens import est

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
C = os.path.join(ROOT, "contracts")

STAGES = [
 dict(id="1A", title="Каталог", doc="11-implementation-progress.md", migs="02598, 02599", gen="generate-billing-catalog.py",
      deps="—", tables=["tables.json"], funcs=[], sql=[]),
 dict(id="1B1", title="Quota kernel (учёт квот, shadow)", doc="12-quota-kernel.md", migs="02600, 02601", gen="generate-billing-quota.py",
      deps="1A", tables=["quota-tables.json"], funcs=["quota-functions.json"], sql=["quota-functions.sql"]),
 dict(id="1B2", title="Standard Apply", doc="13-standard-apply-workflow.md", migs="02602, 02603", gen="generate-billing-workflow.py",
      deps="1B1", tables=["workflow-tables.json"], funcs=["workflow-functions.json"], sql=["workflow-schema.sql", "workflow-functions.sql"]),
 dict(id="1B3", title="SessionAnalysis.Apply и SaveEdits со стилем", doc="14-routed-apply-workflow.md", migs="02604", gen="generate-billing-routed-workflow.py",
      deps="1B2", tables=[], funcs=[], sql=["routed-workflow.sql"]),
 dict(id="1B4", title="V2 render", doc="15-v2-render-workflow.md", migs="02605", gen="generate-billing-render.py",
      deps="1B2", tables=[], funcs=["render-functions.json"], sql=["render-workflow.sql"]),
 dict(id="1B5", title="Editor effects", doc="16-editor-effects-workflow.md", migs="02606", gen="generate-billing-editor.py",
      deps="1B2, 1B4", tables=[], funcs=["editor-functions.json"], sql=["editor-workflow.sql"]),
 dict(id="1B6", title="Отдельные фото-операции", doc="17-standalone-operations.md", migs="02607", gen="generate-billing-operations.py",
      deps="1B2, 1B4, 1B5", tables=[], funcs=["operations-functions.json"], sql=["operations-workflow.sql"]),
 dict(id="1B6p", title="Paid operation funding (исправление)", doc="17-standalone-operations.md и 18-selected-final-operations.md", migs="02608", gen="generate-billing-paid-operation.py",
      deps="1B6", tables=[], funcs=["paid-operation-functions.json"], sql=["paid-operation-funding.sql"]),
 dict(id="1B7", title="Selected/final операции (приёмка НЕ завершена, см. OPEN-08)", doc="18-selected-final-operations.md", migs="02609 (строго после 02608)", gen="generate-billing-selected.py",
      deps="1B6, 1B6p", tables=[], funcs=["selected-functions.json"], sql=["selected-workflow.sql"]),
]

def rd(name):
    return open(os.path.join(C, name), encoding="utf-8").read()

def sql_functions(text):
    """(name, types, [owner, roles]) по ALTER ... OWNER, GRANT и REVOKE в порядке следования в файле."""
    out = {}
    ev = []
    for m in re.finditer(r"ALTER FUNCTION photo\.(\w+)\(([^)]*)\)\s+OWNER TO (\w+)", text):
        ev.append((m.start(), "own", m.group(1), m.group(2), m.group(3)))
    for m in re.finditer(r"GRANT EXECUTE ON FUNCTION photo\.(\w+)\(([^)]*)\)\s+TO ([\w, ]+);", text):
        ev.append((m.start(), "grant", m.group(1), m.group(2), m.group(3)))
    for m in re.finditer(r"REVOKE ALL ON FUNCTION photo\.(\w+)\(([^)]*)\)\s+FROM ([\w, ]+);", text):
        ev.append((m.start(), "revoke", m.group(1), m.group(2), m.group(3)))
    for _, kind, n, a, v in sorted(ev):
        k = (n, a.replace(" ", ""))
        if kind == "own": out[k] = [v, []]
        elif k in out:
            roles = [x.strip() for x in v.split(",")]
            if kind == "grant": out[k][1] = sorted(set(out[k][1]) | set(roles))
            else: out[k][1] = [r for r in out[k][1] if r not in roles]
    return out

def col(c):
    s = f"{c['name']} {c['type']}"
    if c.get("nullable"): s += " NULL"
    if "default" in c: s += f" DEFAULT {c['default']}"
    return s

def build(st):
    L = [f"# BRIEF {st['id']} — {st['title']}", "",
         "Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.", "",
         "## Что читать", f"- Правила: `RULES.md` (всегда).", f"- Описание этапа: `{st['doc']}`.",
         f"- Зависит от этапов: {st['deps']}. Их файлы читать только если задача трогает их функции.", "",
         "## Выпуск", f"- Миграции: {st['migs']}.", f"- Генератор: `scripts/{st['gen']}` (запуск с `--check`).",
         "- Миграции forward-only; старые не править (R9).", ""]
    funcs = []
    for j in st["funcs"]:
        d = json.loads(rd(j))
        for f in d["functions"]:
            funcs.append((f["signature"], f.get("owner", "photo_fn_owner"), f["execute_roles"], j))
    sqlf = {}
    for s in st["sql"]:
        if s.endswith(".sql"): sqlf.update(sql_functions(rd(s)))
    if funcs:
        L += ["## Функции (источник: JSON; ACL = execute_roles)", "", "| Сигнатура | Владелец | EXECUTE |", "|---|---|---|"]
        for sig, own, roles, _ in funcs:
            L.append(f"| `{sig}` | {own} | {', '.join(roles) if roles else 'никто (helper)'} |")
        L.append("")
        names = {s.split("(")[0] + "(" + s.split("(", 1)[1].rstrip(")").replace(" ", "") for s, *_ in funcs}
        extra = [k for k in sqlf if (k[0] + "(" + k[1]) not in names]
        mism = []
        for sig, own, roles, _ in funcs:
            k = (sig.split("(")[0], sig.split("(", 1)[1].rstrip(")").replace(" ", ""))
            if k in sqlf and sqlf[k][1] and sorted(sqlf[k][1]) != sorted(roles): mism.append((sig, roles, sqlf[k][1]))
        if mism:
            L += ["### ВНИМАНИЕ: расхождение JSON и GRANT в SQL (разобрать до работы)", ""]
            L += [f"- `{s}`: JSON {r}, SQL {g}" for s, r, g in mism] + [""]
    elif sqlf:
        L += ["## Функции (источник: SQL, `ALTER … OWNER` и `GRANT EXECUTE`)", "", "| Сигнатура | Владелец | EXECUTE |", "|---|---|---|"]
        for (n, a), (o, r) in sqlf.items():
            L.append(f"| `{n}({a})` | {o} | {', '.join(r) if r else 'никто (helper)'} |")
        L.append("")
    T = []
    for tj in st["tables"]:
        d = json.loads(rd(tj)); T += [f"## Таблицы (источник: `contracts/{tj}`)", ""]
        for t in d["tables"]:
            if "columns" in t:
                T.append(f"### {t['name']}")
                T.append("- Поля: " + "; ".join(col(c) for c in t["columns"]))
                if t.get("primary_key"): T.append(f"- PK: {', '.join(t['primary_key'])}")
                for k in t.get("constraints", []): T.append(f"- {k}")
            else:
                T.append(f"- `{t['name']}` (поля, FK, CHECK — в `contracts/{d.get('ddl_source', 'DDL')}`)")
        T.append("")
    sqltabs = []
    for s in st["sql"]:
        sqltabs += re.findall(r"CREATE TABLE (?:IF NOT EXISTS )?photo\.(\w+)", rd(s))
    if sqltabs and not st["tables"]:
        L += ["## Новые таблицы (определения — в SQL)", ""] + [f"- `{t}`" for t in sorted(set(sqltabs))] + [""]
    L += ["## SQL-источники", ""] + [f"- `contracts/{s}`" for s in (st["sql"] + st["funcs"] + st["tables"])] + [""]
    L += ["## Блокировки (из RULES)", "",
          "- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.",
          "- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.",
          "- R25: не вызывать finalize/settle под task lock.", "",
          "## Перед коммитом", "",
          f"1. `python3 scripts/{st['gen']} --check`" if st["gen"].endswith(".py") else "1. Проверка генератора — см. contracts/README.md",
          "2. `python3 docs/billing/tools/lint-docs.py`",
          "3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.", ""]
    tabfile = None
    if T:
        if est("\n".join(L + T)) > 2000:
            tabfile = f"TABLES-{st['id']}.md"
            L = L[:2] + [f"Таблицы вынесены в `briefs/{tabfile}` (грузить только если задача трогает таблицы).", ""] + L[2:]
        else:
            L = L + T; T = []
    return "\n".join(L), ("\n".join([f"# Таблицы этапа {st['id']}", "", "Сгенерировано `tools/gen-briefs.py`. Вручную не править.", ""] + T) if tabfile else None), tabfile

def main():
    check = "--check" in sys.argv
    od = os.path.join(ROOT, "briefs"); os.makedirs(od, exist_ok=True)
    bad = 0; idx = ["# Карточки этапов", "", "Сгенерированы `tools/gen-briefs.py`. Для задачи загружать: `RULES.md` + одна карточка + файл этапа из неё.", "",
                    "| Карточка | Этап | Миграции |", "|---|---|---|"]
    for st in STAGES:
        name = f"BRIEF-{st['id']}.md"; text, ttext, tfile = build(st)
        for nm, tx in ((name, text), (tfile, ttext)):
            if not nm: continue
            p = os.path.join(od, nm)
            if check:
                if not os.path.exists(p) or open(p, encoding="utf-8").read() != tx: print("drift:", nm); bad += 1
            else: open(p, "w", encoding="utf-8").write(tx)
        idx.append(f"| [{name}]({name}) | {st['title']} | {st['migs']} |")
    if not check: open(os.path.join(od, "README.md"), "w", encoding="utf-8").write("\n".join(idx) + "\n")
    sys.exit(1 if bad else 0)

main()
