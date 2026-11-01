#!/usr/bin/env python3
"""check-covered.py — сверка целевых функций (04) с реализованными (contracts/).

Использование:
  python3 docs/billing/tools/check-covered.py
  python3 docs/billing/tools/check-covered.py --backlog        # сверить и с задачами backlog
  python3 docs/billing/tools/check-covered.py --json           # вывод в JSON

Назначение: перед задачей *-F* проверить строку «возможно покрыта» (backlog/README.md).
Скрипт находит функции из 04-functions-and-api.md, которые уже есть в contracts/,
и функции из contracts/, которых нет в 04-target (реализованы под другими именами).

Выход кодов: 0 = успех, 1 = ошибка разбора файлов.
"""

import argparse
import glob
import json
import re
import sys
from pathlib import Path

BILLING_DIR = Path(__file__).parent.parent
CONTRACTS_DIR = BILLING_DIR / "contracts"
DOC_04 = BILLING_DIR / "04-functions-and-api.md"
BACKLOG_DIR = BILLING_DIR / "backlog"


def load_contracts() -> dict[str, str]:
    """Вернуть {имя_функции: scope} из всех *-functions.json."""
    result: dict[str, str] = {}
    for path in sorted(CONTRACTS_DIR.glob("*-functions.json")):
        try:
            data = json.loads(path.read_text())
        except Exception as e:
            print(f"ERROR reading {path}: {e}", file=sys.stderr)
            sys.exit(1)
        scope = data.get("scope", path.stem)
        for fn in data.get("functions", []):
            sig = fn.get("signature", "")
            name = sig.split("(")[0].strip()
            if name:
                result[name] = scope
    return result


def load_target_04() -> list[str]:
    """Вернуть имена функций из таблиц в 04-functions-and-api.md."""
    text = DOC_04.read_text()
    names = []
    for m in re.finditer(r"^\| (billing_\w+)", text, re.MULTILINE):
        name = m.group(1)
        if name not in names:
            names.append(name)
    return names


def load_backlog_functions() -> dict[str, str]:
    """Вернуть {имя_функции: task_id} из задач *-F* в backlog/."""
    result: dict[str, str] = {}
    # Ищем паттерн "### FIN-F01" или "billing_xxx" в строке с function name
    fn_task_re = re.compile(r"^### (\w+-F\d+)", re.MULTILINE)
    fn_name_re = re.compile(r"`(billing_\w+)`")
    for path in sorted(BACKLOG_DIR.glob("BACKLOG-*.md")):
        text = path.read_text()
        for task_m in fn_task_re.finditer(text):
            task_id = task_m.group(1)
            # Ищем имя функции в следующих 10 строках после заголовка задачи
            start = task_m.start()
            snippet = text[start: start + 800]
            fn_m = fn_name_re.search(snippet)
            if fn_m:
                fname = fn_m.group(1)
                if fname not in result:
                    result[fname] = task_id
    return result


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--backlog", action="store_true",
                        help="Сверить и с задачами backlog/BACKLOG-*.md")
    parser.add_argument("--json", action="store_true",
                        help="Вывести результат в JSON")
    args = parser.parse_args()

    impl = load_contracts()      # реализовано в contracts/
    target = load_target_04()    # целевые имена из 04

    covered = sorted(n for n in target if n in impl)
    only_target = sorted(n for n in target if n not in impl)
    only_impl = sorted(n for n in impl if n not in target)

    backlog_fns: dict[str, str] = {}
    if args.backlog:
        backlog_fns = load_backlog_functions()

    if args.json:
        out = {
            "impl_count": len(impl),
            "target_count": len(target),
            "covered": [{"name": n, "scope": impl[n]} for n in covered],
            "only_in_target_not_implemented": only_target,
            "only_in_contracts_not_in_04": [
                {"name": n, "scope": impl[n]} for n in only_impl
            ],
        }
        if args.backlog:
            out["backlog_fn_tasks"] = {
                k: v for k, v in backlog_fns.items()
            }
            out["backlog_covered_by_contracts"] = sorted(
                n for n in backlog_fns if n in impl
            )
        print(json.dumps(out, ensure_ascii=False, indent=2))
        return

    # Текстовый вывод
    print(f"Реализовано в contracts/ : {len(impl)}")
    print(f"Целевых имён в 04        : {len(target)}")
    print()

    if covered:
        print(f"=== Покрыто ({len(covered)}) — имя совпадает в 04 и contracts ===")
        for n in covered:
            print(f"  {n}  [scope: {impl[n]}]")
        print()

    if only_target:
        print(f"=== Ещё не в contracts ({len(only_target)}) — реализовать ===")
        for n in only_target:
            note = ""
            if args.backlog and n in backlog_fns:
                note = f"  [задача: {backlog_fns[n]}]"
            print(f"  {n}{note}")
        print()

    if only_impl:
        print(
            f"=== Только в contracts ({len(only_impl)}) — реализовано под другими именами ===\n"
            f"    (перед задачей *-F* проверьте эти функции — возможно, цель уже покрыта)"
        )
        for n in only_impl:
            print(f"  {n}  [scope: {impl[n]}]")
        print()

    if args.backlog and backlog_fns:
        backlog_covered = sorted(n for n in backlog_fns if n in impl)
        if backlog_covered:
            print(f"=== Задачи backlog *-F*, возможно покрытые contracts ({len(backlog_covered)}) ===")
            for n in backlog_covered:
                print(f"  {n}  [задача: {backlog_fns[n]}] [scope: {impl[n]}]")
            print()


if __name__ == "__main__":
    main()
