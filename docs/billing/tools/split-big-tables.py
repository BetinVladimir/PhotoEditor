#!/usr/bin/env python3
"""Делит большие таблицы в секциях > LIMIT токенов на части ### «Часть k из m» с повтором шапки.
Строки таблицы не меняются. Использование: python3 tools/split-big-tables.py [--write] [--limit 3500] [--chunk 1800]"""
import re, sys, glob, os
HERE = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, HERE)
from tokens import est
ROOT = os.path.normpath(os.path.join(HERE, ".."))
arg = lambda n, d: int(sys.argv[sys.argv.index(n) + 1]) if n in sys.argv else d
LIMIT, CHUNK, WRITE = arg("--limit", 3500), arg("--chunk", 1800), "--write" in sys.argv
def run(text):
    lines = text.split("\n"); out = []; i = 0; n_split = 0
    # границы секций ##
    idx = [k for k, l in enumerate(lines) if re.match(r"^## ", l)] + [len(lines)]
    out += lines[:idx[0]]
    for a, b in zip(idx, idx[1:]):
        sec = lines[a:b]
        if est("\n".join(sec)) <= LIMIT: out += sec; continue
        tab = [k for k, l in enumerate(sec) if l.startswith("|")]
        if len(tab) < 6: out += sec; continue
        s, e = tab[0], tab[-1] + 1
        if any(not sec[k].startswith("|") for k in range(s, e)): out += sec; continue  # таблица не сплошная
        head, rows = sec[s:s + 2], sec[s + 2:e]
        chunks = []; cur = []
        for r in rows:
            cur.append(r)
            if est("\n".join(head + cur)) >= CHUNK: chunks.append(cur); cur = []
        if cur: chunks.append(cur)
        if len(chunks) < 2: out += sec; continue
        out += sec[:s]
        for k, c in enumerate(chunks, 1):
            out += ["", "### Часть %d из %d" % (k, len(chunks)), ""] + head + c
        out += sec[e:]; n_split += 1
    return "\n".join(out), n_split
for f in sorted(glob.glob(os.path.join(ROOT, "[0-9][0-9]-*.md"))):
    t = open(f, encoding="utf-8").read(); n, c = run(t)
    if c:
        r0 = sum(1 for l in t.split("\n") if l.startswith("|")); r1 = sum(1 for l in n.split("\n") if l.startswith("|"))
        print(os.path.basename(f), "таблиц разделено:", c, "| строк таблиц было/стало (шапки добавляются):", r0, r1)
        if WRITE: open(f, "w", encoding="utf-8").write(n)
print("готово" if WRITE else "пробный прогон")
