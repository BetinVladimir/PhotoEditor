#!/usr/bin/env python3
"""Собирает пакет контекста для малой модели и проверяет бюджет.
Пример:
  python3 tools/build-pack.py --task T-01 --brief 1B7 --section "18#Admission" --func billing_bind_operations --out /tmp/pack.md
Параметры:
  --task T-NN         блок задачи из TASKS.md
  --brief ID          карточка briefs/BRIEF-ID.md
  --section NN#текст  секция файла NN-*.md, заголовок содержит «текст» (можно несколько раз)
  --rows слово        для таблиц в секциях оставить только строки с этим словом (можно несколько раз)
  --func имя          определение SQL-функции и её ACL из contracts/*.sql (можно несколько раз)
  --budget N          потолок материалов в токенах (по умолчанию 14000)
  --window N          окно модели (по умолчанию 32000), --reserve N резерв на ответ (по умолчанию 9000)
Код выхода 2, если не влезает."""
import re, sys, os, glob, argparse
HERE = os.path.dirname(os.path.abspath(__file__)); ROOT = os.path.normpath(os.path.join(HERE, ".."))
sys.path.insert(0, HERE)
from tokens import est, sections
def rd(p): return open(os.path.join(ROOT, p), encoding="utf-8").read()
ap = argparse.ArgumentParser()
ap.add_argument("--task"); ap.add_argument("--brief"); ap.add_argument("--section", action="append", default=[])
ap.add_argument("--rows", action="append", default=[]); ap.add_argument("--func", action="append", default=[])
ap.add_argument("--budget", type=int, default=14000); ap.add_argument("--window", type=int, default=32000)
ap.add_argument("--reserve", type=int, default=9000); ap.add_argument("--out")
a = ap.parse_args()
parts = []  # (название, текст)
prompt = re.search(r"```\n(Ты исполнитель.*?)```", rd("QWEN-PROMPT.md"), re.S)
fixed = [("системный промпт", prompt.group(1).strip() if prompt else ""), ("RULES.md", rd("RULES.md"))]
if a.task:
    src = [rd("TASKS.md")] + [rd(os.path.relpath(f, ROOT)) for f in sorted(glob.glob(os.path.join(ROOT, "backlog", "BACKLOG-*.md")))]
    m = None
    for s in src:
        m = re.search(r"(###? %s\b.*?)(?=\n###? |\Z)" % re.escape(a.task), s, re.S)
        if m: break
    if not m: sys.exit("задача %s не найдена в TASKS.md и backlog/" % a.task)
    fixed.append(("задача " + a.task, m.group(1)))
if a.brief: parts.append(("карточка " + a.brief, rd("briefs/BRIEF-%s.md" % a.brief)))
for s in a.section:
    num, _, key = s.partition("#"); fs = glob.glob(os.path.join(ROOT, "%s-*.md" % num))
    if not fs: sys.exit("нет файла %s-*.md" % num)
    secs = sections(open(fs[0], encoding="utf-8").read()); hit = []
    for k, (h, l, tx) in enumerate(secs):
        if key.lower() in h.lower():
            tx2 = tx
            if l == 2:  # секция ## включает свои подсекции ###
                for h2, l2, t2 in secs[k + 1:]:
                    if l2 <= 2: break
                    tx2 += t2
            hit.append(tx2)
    hit = hit[:1] if hit and any(key.lower() == s[0].lower() for s in secs) else hit
    if not hit: sys.exit("секция «%s» не найдена в %s" % (key, os.path.basename(fs[0])))
    for t in hit:
        if a.rows:
            keep = []
            for ln in t.split("\n"):
                if ln.startswith("|") and not re.match(r"^\|[\s\-|:]+\|$", ln):
                    if any(r.lower() in ln.lower() for r in a.rows) or not keep or not keep[-1].startswith("|"): keep.append(ln)
                else: keep.append(ln)
            t = "\n".join(keep)
        parts.append(("%s#%s" % (os.path.basename(fs[0]), key), t))
for spec in a.func:
    fname, _, name = spec.rpartition(":")
    cand = [os.path.join(ROOT, "contracts", fname)] if fname else sorted(glob.glob(os.path.join(ROOT, "contracts", "*.sql")))
    found = []
    for f in cand:
        if not os.path.exists(f): sys.exit("нет файла contracts/%s" % fname)
        t = open(f, encoding="utf-8").read()
        for m in re.finditer(r"CREATE (?:OR REPLACE )?FUNCTION photo\.%s\(" % re.escape(name), t):
            tag = re.search(r"\bAS\s+(\$\w*\$)", t[m.end():m.end() + 3000])
            if tag:
                s = m.end() + tag.end(); e = t.find(tag.group(1), s)
                body = t[m.start(): (e + len(tag.group(1)) + 1 if e >= 0 else m.end() + 4000)]
            else: body = t[m.start(): m.end() + 2500]
            acl = "\n".join(l.strip() for l in t.split("\n") if re.search(r"^\s*(ALTER|REVOKE|GRANT) .*FUNCTION photo\.%s\(" % re.escape(name), l))
            found.append((os.path.basename(f), body + "\n" + acl))
    if not found: sys.exit("функция %s не найдена" % spec)
    if len(found) > 1 and not fname:
        sys.exit("функция %s есть в нескольких файлах: %s — укажите файл:имя" % (name, ", ".join(sorted({x[0] for x in found}))))
    for f, body in found: parts.append(("%s:%s" % (f, name), body))
rows = [(n, est(t)) for n, t in fixed + parts]
mat = sum(e for (n, e) in rows[len(fixed):]); tot = sum(e for _, e in rows)
free = a.window - tot
print("состав пакета (оценка токенов):")
for n, e in rows: print("  %6d  %s" % (e, n))
print("  материалы: %d из %d; всего во входе: %d; свободно на ответ: %d (нужно ≥ %d)" % (mat, a.budget, tot, free, a.reserve))
bad = []
if mat > a.budget: bad.append("материалы больше бюджета")
if free < a.reserve: bad.append("не хватает места на ответ")
if bad: print("ОШИБКА:", "; ".join(bad), "— режьте задачу или берите меньше секций/строк"); sys.exit(2)
if a.out:
    open(a.out, "w", encoding="utf-8").write("\n\n---\n\n".join("<!-- %s -->\n%s" % (n, t) for n, t in fixed + parts)); print("записано:", a.out)
