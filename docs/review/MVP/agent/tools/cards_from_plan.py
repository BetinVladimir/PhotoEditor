#!/usr/bin/env python3
"""Turn an approved decomposition plan (notes/<AREA>-plan.md) into extraction cards. No model involved.

  python3 docs/review/MVP/agent/tools/cards_from_plan.py docs/review/MVP/agent/notes/P-9-plan.md [--dry-run]

The plan contains one fenced block ```plan ... ``` :
  area: P-9                      card ids become <area>x-<id>
  file: apps/photographer/screens/editor/[version].tsx
  lang: ts | go | py
  domain: photographer           value of the card's `area:` field
  gate: <area>-plan              card that must be done first (the merge card)
  verify: <shell command>        (one or more lines)
  ---
  id | title | start-end | target file | name | signature/notes
Rules enforced here: ranges inside the file, non-overlapping, each <= 150 lines, target files distinct
or '-' (same file as source). Cards are chained BOTTOM-UP (highest line range first) so earlier line numbers
stay valid while earlier cards run. Exit 2 = refused.
"""
import pathlib, re, sys

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[4]
TASKS = HERE.parent / 'tasks'

TEMPLATE = {
 'go': ('Extract the statements on lines {a}-{b} of `{file}` into a new function/method `{name}` in `{target}`.\n'
        'Replace the original range by a call to it. The statements keep their exact order and text; variables the range reads from outside become parameters, variables it declares and the rest of the function uses become return values (see the hint).'),
 'ts': ('Extract lines {a}-{b} of `{file}` into `{name}` in `{target}` (a hook if it calls `use*` functions or React state, otherwise a plain function/component).\n'
        'Replace the original range by a call to it. Code inside keeps its exact text and order; free variables become parameters, values used afterwards are returned.'),
 'py': ('Extract the statements on lines {a}-{b} of `{file}` into a new function `{name}` in `{target}`.\n'
        'Prefer `python3 docs/review/MVP/agent/tools/extract_stmts.py` (see its --help) over retyping.'),
}


def die(m):
    print('REFUSED: ' + m, file=sys.stderr); sys.exit(2)


def main():
    args = [x for x in sys.argv[1:] if not x.startswith('--')]
    dry = '--dry-run' in sys.argv
    if len(args) != 1: die(__doc__)
    text = pathlib.Path(args[0]).read_text()
    m = re.search(r'```plan\n(.*?)```', text, re.S)
    if not m: die('no ```plan block')
    head, _, rows = m.group(1).partition('\n---\n')
    meta = {'verify': []}
    for l in head.splitlines():
        if ':' in l:
            k, v = l.split(':', 1)
            if k.strip() == 'verify': meta['verify'].append(v.strip())
            else: meta[k.strip()] = v.strip()
    for k in ('area', 'file', 'lang', 'gate'):
        if k not in meta: die('missing header key ' + k)
    if meta['lang'] not in TEMPLATE: die('lang must be go|ts|py')
    if not meta['verify']: die('at least one verify: line')
    src = ROOT / meta['file']
    if not src.is_file(): die('file not found: ' + meta['file'])
    nlines = len(src.read_text().split('\n'))
    items = []
    for l in rows.strip().splitlines():
        p = [x.strip() for x in l.split('|')]
        if len(p) != 6: die('row needs 6 columns: ' + l)
        cid, title, rng, target, name, hint = p
        mm = re.fullmatch(r'(\d+)-(\d+)', rng)
        if not mm: die('bad range: ' + l)
        a, b = int(mm.group(1)), int(mm.group(2))
        if not (1 <= a <= b <= nlines): die('range outside file (%d lines): %s' % (nlines, l))
        if b - a + 1 > 150: die('range longer than 150 lines, split it: ' + l)
        if not re.fullmatch(r'[A-Za-z0-9]+', cid): die('id must be alphanumeric: ' + cid)
        items.append(dict(id=cid, title=title, a=a, b=b, target=meta['file'] if target == '-' else target, name=name, hint=hint))
    if not items: die('no rows')
    if len({i['id'] for i in items}) != len(items): die('duplicate ids')
    items.sort(key=lambda i: -i['a'])
    for hi, lo in zip(items, items[1:]):
        if lo['b'] >= hi['a']: die('ranges overlap: %s and %s' % (lo['id'], hi['id']))
    tgts = [i['target'] for i in items if i['target'] != meta['file']]
    if len(set(tgts)) != len(tgts): die('two rows write the same new target file (use one row per file)')
    prev = meta['gate']
    out = []
    for i in items:
        cid = '%sx-%s' % (meta['area'], i['id'])
        new = i['target'] != meta['file']
        edit = [meta['file']] + ([i['target'] + ' (new)'] if new else [])
        body = TEMPLATE[meta['lang']].format(a=i['a'], b=i['b'], file=meta['file'], name=i['name'], target=i['target'])
        card = ('---\nid: %s\ntitle: %s\narea: %s\nsize: S\ndepends: [%s]\nedit:\n%s\ncontext:\n  - %s:%d-%d\nverify:\n%s\n---\n'
                '## Goal\n%s\n\n## Hint from the plan\n%s\n\n## Steps\n'
                '1. Read the range (it is in the context above). Create the target and move the code; do not rewrite logic.\n'
                '2. Run the verify commands. Allowed fixes: imports, parameter/return plumbing. Nothing else.\n\n'
                '## Do not\n- Do not change behaviour, rename anything outside the new function, or touch other line ranges.\n'
                '- If the range cannot be extracted without changing control flow (`return`/`break` out of the range, labels, `defer`), answer `BLOCKED: <reason>` and stop.\n'
                ) % (cid, i['title'], meta.get('domain', 'misc'), prev,
                     '\n'.join('  - ' + e for e in edit), meta['file'], i['a'], i['b'],
                     '\n'.join('  - ' + v for v in meta['verify']), body, i['hint'])
        out.append((cid, card)); prev = cid
    print('%d cards, chained bottom-up from line %d to %d' % (len(out), items[0]['a'], items[-1]['a']))
    for cid, card in out:
        path = TASKS / (cid + '.md')
        if path.exists(): die('%s already exists' % path)
    if dry:
        print('dry-run: nothing written'); return
    for cid, card in out:
        (TASKS / (cid + '.md')).write_text(card); print('wrote', cid)


if __name__ == '__main__':
    main()
