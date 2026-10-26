#!/usr/bin/env python3
"""Move methods of `class OwnerAPI` into module-level functions (cards P-5*). No model involved.

  python3 .../move_methods.py --list
  python3 .../move_methods.py --domain interchange --methods a,b,c [--dry-run]

A moved method `async foo(a:string,b?:X){BODY}` becomes, in src/api/<domain>.ts,
`export async function foo(self:OwnerAPI,a:string,b?:X){BODY'}` where BODY' has `this.` replaced by `self.`,
and the class keeps a one-line facade `async foo(a:string,b?:X){return api_<domain>.foo(this,a,b);}`.
Preconditions (card P-5p): client/tenant/path/get/mutate are public, src/api/shared.ts exports requireArchiveUUID.
Refuses (exit 2, nothing written) on: unknown/duplicate names, generic or private methods, methods using private
members, unbalanced text, a target file that exists, or helpers it cannot resolve.
"""
import argparse, pathlib, re, sys

SRC = pathlib.Path('apps/photographer/src/owner-api.ts')
HELPERS = {'requireArchiveUUID': './shared'}


def die(m):
    print('REFUSED: ' + m, file=sys.stderr); sys.exit(2)


def noise(line):
    out, i, n = [], 0, len(line)
    while i < n:
        c = line[i]
        if c == '/' and i + 1 < n and line[i + 1] == '/':
            break
        if c in '"\'`':
            j = i + 1
            while j < n and line[j] != c:
                j += 2 if line[j] == '\\' else 1
            if j >= n:
                die('unterminated string on a line: %r' % line[:80])
            i = j + 1
            continue
        out.append(c); i += 1
    return ''.join(out)


def match_close(s, i, open_, close):
    d = 0
    for j in range(i, len(s)):
        if s[j] == open_: d += 1
        elif s[j] == close:
            d -= 1
            if d == 0: return j
    return -1


def split_params(p):
    parts, d, cur = [], 0, ''
    q = p.replace('=>', '__A__')
    for ch in q:
        if ch in '([{<': d += 1
        elif ch in ')]}>': d -= 1
        if ch == ',' and d == 0:
            parts.append(cur); cur = ''
        else: cur += ch
    if cur.strip(): parts.append(cur)
    return [x.replace('__A__', '=>') for x in parts]


def parse(L):
    cs = next((i for i, l in enumerate(L) if l.startswith('export class OwnerAPI')), None)
    if cs is None: die('class OwnerAPI not found')
    ce = next(i for i in range(len(L) - 1, cs, -1) if L[i] == '}')
    meths, i = [], cs + 1
    head = re.compile(r'^  ((?:private |protected |static |readonly )*)(async )?([A-Za-z_]\w*)\s*(<)?\(')
    while i < ce:
        m = head.match(L[i])
        if not m or m.group(3) in ('constructor',):
            i += 1; continue
        text_lines = []
        bal, j = 0, i
        started = False
        while j < ce:
            t = noise(L[j])
            text_lines.append(L[j])
            bal += t.count('{') - t.count('}') + t.count('(') - t.count(')') + t.count('[') - t.count(']')
            if '{' in t: started = True
            if started and bal == 0: break
            j += 1
        else:
            die('cannot find the end of %s' % m.group(3))
        meths.append(dict(name=m.group(3), mods=m.group(1).strip(), is_async=bool(m.group(2)), generic=bool(m.group(4)),
                          start=i, end=j, text='\n'.join(text_lines)))
        i = j + 1
    return cs, ce, meths


def analyse(m, privates):
    t = m['text']
    k = t.index('(', t.index(m['name']))
    c = match_close(t, k, '(', ')')
    if c < 0: die('bad parameter list of %s' % m['name'])
    params = t[k + 1:c]
    rest = t[c + 1:]
    b = -1
    if rest.lstrip().startswith(':'):
        r2 = rest.replace('=>', '__')
        d = 0
        for idx, ch in enumerate(r2):
            if ch in '<([': d += 1
            elif ch in '>)]': d -= 1
            elif ch == '{':
                if d == 0 and idx > 0 and r2[:idx].strip() not in (':',): b = idx; break
                if d == 0 and r2[:idx].strip() == ':': die('%s: object-literal return type not supported' % m['name'])
                d += 1
            elif ch == '}': d -= 1
    elif '{' in rest:
        b = rest.index('{')
    if b < 0: die('%s has no body' % m['name'])
    between = rest[:b].strip()
    if between and not between.startswith(':'): die('%s: unexpected text between ) and {' % m['name'])
    body = rest[b:]
    names = []
    for p in split_params(params):
        p = p.strip()
        if not p: continue
        mm = re.match(r'^(?:\.\.\.)?([A-Za-z_]\w*)', p)
        if not mm or p.startswith('{') or p.startswith('['): die('%s: destructured parameter not supported' % m['name'])
        names.append(('...' if p.startswith('...') else '') + mm.group(1))
    return params, between, body, names


def prepare(path, L, dry):
    helper = [i for i, l in enumerate(L) if l.startswith('function requireArchiveUUID(')]
    if len(helper) != 1: die('expected exactly one top-level requireArchiveUUID (already prepared?)')
    src = next((re.match(r'^import \{[^}]*\barchiveUUID\b[^}]*\} from [\'"](.+)[\'"]', l).group(1) for l in L if re.match(r'^import \{[^}]*\barchiveUUID\b[^}]*\} from', l)), None)
    if src is None: die('import of archiveUUID not found')
    shared = path.parent / 'api' / 'shared.ts'
    if shared.exists(): die('%s already exists' % shared)
    out = []
    n_priv = 0
    for i, l in enumerate(L):
        if i == helper[0]:
            continue
        if l.startswith('  constructor(private client'):
            l = l.replace('constructor(private client', 'constructor(readonly client', 1); n_priv += 1
        elif re.match(r'^  private ', l):
            l = l[:2] + l[2 + len('private '):]; n_priv += 1
        out.append(l)
    last = max(i for i, l in enumerate(out) if l.startswith('import '))
    out.insert(last + 1, "import {requireArchiveUUID} from './api/shared';")
    text = ("import {archiveUUID} from '%s';\n\nexport function requireArchiveUUID(value:unknown):string{if(!archiveUUID(value))throw new Error('SCOPE_INVALID');return value as string;}\n" % ('../' + src[2:] if src.startswith('./') else src))
    print('removed `private` from %d members; requireArchiveUUID -> %s' % (n_priv, shared))
    if dry:
        print('dry-run: nothing written'); return
    shared.parent.mkdir(exist_ok=True)
    shared.write_text(text)
    path.write_text('\n'.join(out))
    print('done. Now run: cd apps/photographer && npx tsc --noEmit')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--list', action='store_true')
    ap.add_argument('--prepare', action='store_true')
    ap.add_argument('--domain'); ap.add_argument('--methods')
    ap.add_argument('--dry-run', action='store_true')
    ap.add_argument('--file', default=str(SRC))
    a = ap.parse_args()
    path = pathlib.Path(a.file)
    L = path.read_text().split('\n')
    if a.prepare:
        return prepare(path, L, a.dry_run)
    cs, ce, meths = parse(L)
    privates = {m['name'] for m in meths if m['mods'].startswith(('private', 'protected'))}
    byname = {}
    for m in meths:
        if m['name'] in byname: die('duplicate method name %s' % m['name'])
        byname[m['name']] = m
    if a.list:
        for m in meths:
            members = sorted(set(re.findall(r'this\.([A-Za-z_]\w*)', m['text'])))
            flags = ('G' if m['generic'] else '') + ('P' if m['name'] in privates else '')
            print('%-34s lines %4d-%4d %5d chars %-2s uses %s' % (m['name'], m['start'] + 1, m['end'] + 1, len(m['text']), flags, ','.join(members)))
        return
    if not a.domain or not a.methods: die('--domain and --methods are required')
    if not re.fullmatch(r'[a-z][a-z0-9]*', a.domain): die('--domain must be lowercase letters/digits')
    want = [x for x in a.methods.split(',') if x]
    if len(set(want)) != len(want): die('duplicate in --methods')
    out_path = path.parent / 'api' / (a.domain + '.ts')
    if out_path.exists(): die('%s already exists' % out_path)
    shared = path.parent / 'api' / 'shared.ts'
    imports = {}
    for l in L[:cs]:
        mm = re.match(r'^import (type )?\{(.*)\} from [\'"](.+)[\'"];?\s*$', l)
        if mm:
            imports[l] = (bool(mm.group(1)), [x.strip() for x in mm.group(2).split(',') if x.strip()], mm.group(3))
    funcs, facades, bodies = [], {}, []
    for n in want:
        m = byname.get(n)
        if m is None: die('unknown method %s' % n)
        if m['generic']: die('%s is generic' % n)
        if m['name'] in privates: die('%s is private' % n)
        params, between, body, names = analyse(m, privates)
        used = set(re.findall(r'this\.([A-Za-z_]\w*)', body))
        bad = used & privates
        if bad: die('%s uses private member(s) %s; move them first or keep this method' % (n, sorted(bad)))
        moved = set(want)
        nb = re.sub(r'(?<![\w.])this\.', 'self.', body)
        if re.search(r'(?<![\w.])this(?![\w])', nb): die('%s uses bare `this`' % n)
        sig_params = 'self:OwnerAPI' + (',' + params if params.strip() else '')
        ret = between if between else ''
        funcs.append('export %sfunction %s(%s)%s%s' % ('async ' if m['is_async'] else '', n, sig_params, ret, nb))
        facades[n] = '  %s%s(%s)%s{return api_%s.%s(%s);}' % ('async ' if m['is_async'] else '', n, params, ret, a.domain, n, ','.join(['this'] + names))
        bodies.append(params + ' ' + (between or '') + ' ' + body)
    allbody = '\n'.join(bodies)
    need_imports, ident_used = [], lambda w: re.search(r'(?<![\w.$])' + re.escape(w) + r'(?![\w$])', allbody) is not None
    for l, (is_type, items, src) in imports.items():
        keep = []
        for it in items:
            nm = it.split(' as ')[-1].replace('type ', '').strip()
            if ident_used(nm): keep.append(it)
        if keep:
            s2 = src if not src.startswith('./') else '.' + src if src.startswith('./') else src
            s2 = ('../' + src[2:]) if src.startswith('./') else src
            need_imports.append("import %s{%s} from '%s';" % ('type ' if is_type else '', ','.join(keep), s2))
    for h, mod in HELPERS.items():
        if ident_used(h):
            if not shared.exists() and not a.dry_run: die('%s missing (card P-5p must run first)' % shared)
            need_imports.append("import {%s} from '%s';" % (h, mod))
    need_imports.append("import type {OwnerAPI} from '../owner-api';")
    newtext = '\n'.join(need_imports) + '\n\n' + '\n\n'.join(funcs) + '\n'
    # brackets sanity
    tt = ' '.join(noise(x) for x in newtext.split('\n'))
    for o, c in ('{}', '()', '[]'):
        if tt.count(o) != tt.count(c): die('unbalanced %s%s in generated file' % (o, c))
    # rewrite the class
    out = list(L)
    for n in sorted(want, key=lambda x: -byname[x]['start']):
        m = byname[n]
        out[m['start']:m['end'] + 1] = [facades[n]]
    imp_last = max(i for i, l in enumerate(L[:cs]) if l.startswith('import '))
    out.insert(imp_last + 1, "import * as api_%s from './api/%s';" % (a.domain, a.domain))
    print('domain %s: %d methods, %d chars moved' % (a.domain, len(want), sum(len(byname[n]['text']) for n in want)))
    print('new file %s (%d lines)' % (out_path, newtext.count('\n')))
    if a.dry_run:
        print('dry-run: nothing written'); return
    out_path.parent.mkdir(exist_ok=True)
    out_path.write_text(newtext)
    path.write_text('\n'.join(out))
    print('done. Now run: cd apps/photographer && npx tsc --noEmit && npx vitest run src/owner-api')


if __name__ == '__main__':
    main()
