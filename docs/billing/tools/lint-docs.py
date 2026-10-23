#!/usr/bin/env python3
"""Линтер docs/billing: слипшиеся слова и числа, битые символы, повреждённые ссылки.
Использование: python3 tools/lint-docs.py [--fix]   (код выхода 1, если есть замечания)"""
import re, sys, glob, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tokens import est, sections
SEC_LIMIT = 4000   # токенов на секцию md
FILE_LIMIT = 4000  # токенов на файл в briefs/ и RULES/TASKS/EXAMPLES/OPEN/QWEN-PROMPT
ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
GLUE = [(re.compile(r'(?<=[А-Яа-я])(?=\d)'), ' '),      # Миграция02604 -> Миграция 02604
        (re.compile(r'(?<=\d)(?=[а-я]{2,})'), ' ')]      # 7дней -> 7 дней (1Б.2 не трогаем)
fix = "--fix" in sys.argv
bad = 0
for f in sorted(glob.glob(os.path.join(ROOT, "*.md")) + glob.glob(os.path.join(ROOT, "contracts", "*.md")) + glob.glob(os.path.join(ROOT, "briefs", "*.md")) + glob.glob(os.path.join(ROOT, "backlog", "*.md"))):
    t = open(f, encoding="utf-8").read(); n = t
    for p, r in GLUE: n = p.sub(r, n)
    name = os.path.relpath(f, ROOT)
    if "�" in t: print(f"{name}: символ U+FFFD"); bad += 1
    for m in re.finditer(r'\]\(([^)#:]+\.md)(#[^)]*)?\)', t):
        if not os.path.exists(os.path.join(os.path.dirname(f), m.group(1))):
            print(f"{name}: битая ссылка {m.group(1)}"); bad += 1
    # размер секций (только нумерованные спецификации) и коротких файлов
    base = os.path.basename(f)
    if re.match(r"\d\d-", base):
        for h, l, tx in sections(t):
            if est(tx) > SEC_LIMIT: print(f"{name}: секция «{h[:50]}» ~{est(tx)} токенов > {SEC_LIMIT}"); bad += 1
    elif base not in ("README.md", "STATUS.md") and est(t) > FILE_LIMIT:
        print(f"{name}: файл ~{est(t)} токенов > {FILE_LIMIT}"); bad += 1
    if n != t:
        if fix: open(f, "w", encoding="utf-8").write(n); print(f"{name}: исправлено")
        else: print(f"{name}: слипшиеся слова/числа"); bad += 1
sys.exit(1 if bad else 0)
