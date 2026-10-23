#!/usr/bin/env python3
"""Сборщик контекстного пакета карточки (docs/video/tools/ctx.py). Только стандартная библиотека.
  python3 ctx.py TK-640            # напечатать пакет: карточка + нужные разделы ТЗ + срезы векторов
  python3 ctx.py --check           # оценить все карточки; код 1, если пакет какой-либо карточки > бюджета
  python3 ctx.py --slice editor.json compose_at [N]   # срез раздела вектора (первые N случаев; по умолчанию все)
Бюджет рассчитан на модель с окном 32 000 токенов: пакет ≤ BUDGET_TOKENS, остальное — код, рассуждения и ответ."""
import json, os, re, sys

HERE = os.path.dirname(os.path.abspath(__file__))
DOCS = os.path.normpath(os.path.join(HERE, '..'))
VEC = os.path.join(DOCS, 'vectors')
BUDGET_TOKENS = 18000
CHARS_PER_TOKEN = 2.4          # консервативно для русского текста с кодом
PRELUDE = ['README.md#Общие правила документа']
VEC_PREVIEW_CASES = 2          # сколько случаев раздела вектора входит в пакет (остальное — срезом по требованию)


def tokens(n_chars):
    return int(n_chars / CHARS_PER_TOKEN) + 1


def read(p):
    with open(p, encoding='utf-8') as f:
        return f.read()


def docfile(nn):
    for f in os.listdir(DOCS):
        if f.startswith(nn + '-') and f.endswith('.md'):
            return f
    return None


_chunks = {}
MISSING = {}
FN_RANGE_OK = set()   # идентификаторы, определённые диапазоном в каталоге (FN-T-15…28 и т. п.)


def chunks(doc):
    """doc -> {section_id: text}; section_id — '7', '7.1', 'DS-15'; текст включает подразделы."""
    if doc in _chunks:
        return _chunks[doc]
    lines = read(os.path.join(DOCS, doc)).split('\n')
    heads = []
    for i, l in enumerate(lines):
        m = re.match(r'^(#{2,4}) (.*)$', l)
        if m:
            t = m.group(2)
            sid = None
            n = re.match(r'^(\d+(?:\.\d+)*)\.?\s', t)
            d = re.match(r'^(DS-\d+)\.', t)
            if n:
                sid = n.group(1)
            elif d:
                sid = d.group(1)
            heads.append((i, len(m.group(1)), sid, t))
    out = {}
    for k, (i, lvl, sid, t) in enumerate(heads):
        end = len(lines)
        for j, l2, _, _ in heads[k + 1:]:
            if l2 <= lvl:
                end = j
                break
        if sid:
            out.setdefault(sid, '\n'.join(lines[i:end]))
    _chunks[doc] = out
    return out


def fn_line(fid):
    """строка определения функции в каталоге 14 или в нормативных разделах."""
    txt = read(os.path.join(DOCS, '14-function-catalog.md'))
    for l in txt.split('\n'):
        if re.match(r'^(\*\*|\| )' + re.escape(fid) + r'\b', l):
            return l
    return None


def vec_sections():
    res = {}
    for f in os.listdir(VEC):
        if f.endswith('.json'):
            try:
                d = json.loads(read(os.path.join(VEC, f)))
            except Exception:
                continue
            if isinstance(d, dict):
                for k, v in d.items():
                    res.setdefault(k, []).append((f, v))
    return res


def cards():
    s = read(os.path.join(DOCS, '15-task-cards.md'))
    res = {}
    for l in s.split('\n'):
        m = re.match(r'^(?:\*\*|\| )(TK-\d{3})[. |]', l)
        if m and m.group(1) not in res:
            res[m.group(1)] = l
    return res


def refs(card):
    """-> (разделы: список (doc, sid)), (функции), (секции векторов)."""
    secs, fns, vecs = [], [], []
    for m in re.finditer(r'(?:\[(\d\d)\]\([^)]*\)|(?<![\w-])(\d\d)(?![\d\w-]))\s*(?:,\s*)?(?:\)|\])?\s*(?:,\s*)?(?:§|раздел(?:ы|а|ов|е)?\s*|п\.\s*|(?=\d+\.\d+))\s*(\d+(?:\.\d+)?)(?:\s*[–-]\s*(\d+))?', card):
        nn = m.group(1) or m.group(2)
        a, b = m.group(3), m.group(4)
        f = docfile(nn)
        if not f:
            continue
        if b and '.' not in a:
            for x in range(int(a), int(b) + 1):
                secs.append((f, str(x)))
        else:
            secs.append((f, a))
    for m in re.finditer(r'\b(DS-\d+)\b', card):
        secs.append(('13-data-structures.md', m.group(1)))
    for m in re.finditer(r'\bFN-([A-Z])-(\d+)(?:\s*…\s*(?:R-|T-|E-|G-|U-|L-|W-|D-|C-|Q-|P-|X-)?(\d+))?', card):
        a = int(m.group(2))
        b = int(m.group(3)) if m.group(3) else a
        for x in range(a, b + 1):
            fns.append(f'FN-{m.group(1)}-{x:02d}')
    vs = vec_sections()
    for m in re.finditer(r'`([a-z][a-z0-9_]{2,})`', card):
        if m.group(1) in vs:
            vecs.append(m.group(1))
    # упорядоченно, без дублей
    return list(dict.fromkeys(secs)), list(dict.fromkeys(fns)), list(dict.fromkeys(vecs))


COMPOSITE = {'TK-606'}   # выполняются карточками-частями; исполнителю не выдаются


def pack(tk):
    c = cards()
    if tk not in c:
        raise SystemExit('нет карточки ' + tk)
    card = c[tk]
    secs, fns, vecs = refs(card)
    parts = [('карточка ' + tk, card)]
    miss = []
    for f, sid in secs:
        t = chunks(f).get(sid)
        if t:
            parts.append((f'{f}§{sid}', t))
        else:
            miss.append(f'{f}§{sid}')
    seen = {t for _, t in parts}
    for fid in fns:
        l = fn_line(fid)
        if l is None and fid not in FN_RANGE_OK:
            miss.append(fid)
        if l and l not in seen:
            parts.append((fid, l))
            seen.add(l)
    vparts = []
    for k in vecs:
        for f, v in vec_sections().get(k, []):
            if isinstance(v, list):
                pv = json.dumps(v[:VEC_PREVIEW_CASES], ensure_ascii=False)
            else:
                pv = json.dumps(v, ensure_ascii=False)[:1500]
            vparts.append((f'vectors/{f}:{k} ({len(v) if hasattr(v, "__len__") else 1} случаев; срез: ctx.py --slice {f} {k})', pv))
    MISSING[tk] = miss
    return parts, vparts


def size(parts):
    return sum(len(t) for _, t in parts)


def main():
    a = sys.argv[1:]
    if a and a[0] == '--slice':
        d = json.loads(read(os.path.join(VEC, a[1])))
        v = d[a[2]]
        if len(a) > 3 and isinstance(v, list):
            v = v[:int(a[3])]
        print(json.dumps(v, ensure_ascii=False, indent=1))
        return
    if a and a[0] == '--check':
        over = []
        for tk in cards():
            if tk in COMPOSITE:
                continue
            p, v = pack(tk)
            t = tokens(size(p) + size(v) + sum(len(read(os.path.join(DOCS, x.split('#')[0]))) if False else 0 for x in PRELUDE) + 4800)
            if t > BUDGET_TOKENS:
                over.append((t, tk))
        over.sort(reverse=True)
        miss = {k: v for k, v in MISSING.items() if v}
        print(f'карточек с неразрешёнными ссылками: {len(miss)}')
        for k, v in sorted(miss.items()):
            print('  ', k, ', '.join(v))
        sizes = sorted(((tokens(size(pack(k)[0]) + size(pack(k)[1])), k) for k in cards() if k not in COMPOSITE), reverse=True)[:5]
        print('самые тяжёлые пакеты:', sizes)
        print(f'карточек: {len(cards())}; бюджет {BUDGET_TOKENS} ток.; сверх бюджета: {len(over)}')
        for t, tk in over:
            print(f'  {tk}: ~{t} ток.')
        sys.exit(1 if over else 0)
    if a[0] in COMPOSITE:
        raise SystemExit(a[0] + ' — составная карточка: выдавать её части (см. 15)')
    p, v = pack(a[0])
    for name, t in p + v:
        print(f'\n===== {name} =====\n{t}')
    print(f'\n[оценка: ~{tokens(size(p) + size(v))} токенов без запаса 4800 на пример/код]')


if __name__ == '__main__':
    main()
