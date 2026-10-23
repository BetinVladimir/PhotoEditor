#!/usr/bin/env python3
"""Режет длинные абзацы md на строки-правила (одно предложение = один пункт списка).
Не трогает заголовки, таблицы, блоки кода, списки, цитаты. Текст не меняется, только разбивка. Идемпотентен.
Использование: python3 tools/split-paragraphs.py [--write] [--min 300]"""
import re, sys, glob, os
ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
MIN = int(sys.argv[sys.argv.index("--min") + 1]) if "--min" in sys.argv else 300
WRITE = "--write" in sys.argv
ABBR = re.compile(r'(?:т\.\s?е|т\.\s?д|т\.\s?п|т\.\s?н|напр|см|рис|ср|др|пр|вкл|мин|макс|e\.g|i\.e|vs|etc|No|approx)\.$', re.I)
SPLIT = re.compile(r'(?<=[.;!?])\s+(?=[А-ЯЁA-Z`«"(\[])')
def sentences(p):
    parts = SPLIT.split(p); out = []
    for s in parts:
        if out and (ABBR.search(out[-1]) or re.search(r'\b\d+\.$', out[-1]) or re.search(r'\b[vV]\d*\.$', out[-1])): out[-1] += " " + s
        else: out.append(s)
    return out
def process(text):
    out = []; incode = False; changed = 0
    for line in text.split("\n"):
        if line.strip().startswith("```"): incode = not incode; out.append(line); continue
        plain = (not incode and len(line) > MIN and not re.match(r'^\s*([#|>\-*]|\d+[.)]\s)', line))
        if not plain: out.append(line); continue
        ss = sentences(line.strip())
        if len(ss) < 2: out.append(line); continue
        out += ["- " + s for s in ss]; changed += 1
    return "\n".join(out), changed
tot = 0
for f in sorted(glob.glob(os.path.join(ROOT, "[0-9][0-9]-*.md"))):
    t = open(f, encoding="utf-8").read(); n, c = process(t)
    if c:
        tot += c; print(f"{os.path.basename(f)}: абзацев разбито {c}, строк {t.count(chr(10))+1} -> {n.count(chr(10))+1}")
        if WRITE: open(f, "w", encoding="utf-8").write(n)
print("всего абзацев:", tot, "(записано)" if WRITE else "(пробный прогон)")
