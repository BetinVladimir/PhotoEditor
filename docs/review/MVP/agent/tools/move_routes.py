#!/usr/bin/env python3
"""Move a contiguous block of route registrations out of catalogRoutes into its own method.

Mechanical refactoring helper for cards B-5*. It does NOT use a model: it cuts text by anchors,
infers the parameters (outer local variables that the block reads), writes the new file, replaces
the block by one call, and fixes the import lists of both files.

Usage (from the repository root):
  python3 docs/review/MVP/agent/tools/move_routes.py --name Capture \
      --first 'r.Get("/api/v1/tenants/{t}/shoot-sessions/{id}/alerts"' \
      --last  'r.Post("/api/v1/tenants/{t}/capture-uploads/{id}/complete"' [--dry-run]

--first  unique substring of the FIRST line of the block (a route or a declaration before the routes)
--last   unique substring of the line where the LAST route statement of the block starts;
         the block ends where that statement ends (parentheses balance back to zero)
Exit code 0 = done / dry-run ok, 2 = refused (nothing written), with the reason on stderr.
"""
import argparse
import pathlib
import re
import sys

SRC = pathlib.Path('backend/internal/httpapi/catalog.go')
FUNC_RE = re.compile(r'^func \(s \*Server\) catalogRoutes\(r chi\.Router\) \{')


def die(msg):
    print('REFUSED: ' + msg, file=sys.stderr)
    sys.exit(2)


def strip_noise(line):
    """Remove string/rune literals and // comments so that counting brackets is reliable."""
    out, i, n = [], 0, len(line)
    while i < n:
        c = line[i]
        if c == '/' and i + 1 < n and line[i + 1] == '/':
            break
        if c in '"\'':
            j = i + 1
            while j < n and line[j] != c:
                j += 2 if line[j] == '\\' else 1
            i = j + 1
            continue
        if c == '`':
            j = line.find('`', i + 1)
            if j < 0:
                break  # raw string continues on the next line; ignore the rest
            i = j + 1
            continue
        out.append(c)
        i += 1
    return ''.join(out)


def import_name(alias, ipath):
    if alias:
        return alias
    return 'chi' if ipath.endswith('/v5') else ipath.rsplit('/', 1)[-1]


def split_imports(lines):
    start = next(i for i, l in enumerate(lines) if l.startswith('import ('))
    end = next(i for i in range(start, len(lines)) if lines[i].strip() == ')')
    items = []
    for l in lines[start + 1:end]:
        m = re.match(r'^\s*(?:(\w+)\s+)?"([^"]+)"\s*$', l)
        if m:
            items.append((import_name(m.group(1), m.group(2)), m.group(2), m.group(1)))
    return start, end, items


def uses(name, text):
    return re.search(r'(?<![\w.])' + re.escape(name) + r'\.', text) is not None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--name', required=True)
    ap.add_argument('--first', required=True)
    ap.add_argument('--last', required=True)
    ap.add_argument('--dry-run', action='store_true')
    ap.add_argument('--file', default=str(SRC))
    a = ap.parse_args()
    if not re.fullmatch(r'[A-Z][A-Za-z0-9]*', a.name):
        die('--name must be CamelCase')
    path = pathlib.Path(a.file)
    text = path.read_text()
    L = text.split('\n')

    fstart = next((i for i, l in enumerate(L) if FUNC_RE.match(l)), None)
    if fstart is None:
        die('catalogRoutes not found')
    fend = next(i for i in range(fstart + 1, len(L)) if L[i] == '}')

    def find_unique(sub, what):
        hits = [i for i in range(fstart + 1, fend) if sub in L[i]]
        if len(hits) != 1:
            die('%s anchor %r matches %d lines (need exactly 1)' % (what, sub, len(hits)))
        return hits[0]

    s = find_unique(a.first, '--first')
    l0 = find_unique(a.last, '--last')
    if l0 < s:
        die('--last is above --first')
    bal, e = 0, None
    for i in range(l0, fend):
        t = strip_noise(L[i])
        bal += t.count('(') + t.count('{') - t.count(')') - t.count('}')
        if bal == 0:
            e = i
            break
    if e is None:
        die('could not find the end of the last statement')
    block = L[s:e + 1]
    btxt = '\n'.join(block)
    clean = [strip_noise(x) for x in block]
    if sum(c.count('{') - c.count('}') + c.count('(') - c.count(')') for c in clean) != 0:
        die('block is not balanced')
    # outer locals: top-level declarations of catalogRoutes that are OUTSIDE the block
    decl = {}
    for i in range(fstart + 1, fend):
        l = L[i]
        if l.startswith('\t\t') or l.startswith('  \t'):
            continue
        m = re.match(r'^[ \t]{1}(\w+) := ([\w.]+)\{', l) or re.match(r'^[ \t]{1}var (\w+) ([\w.\[\]*]+)\s*$', l)
        if m:
            decl.setdefault(m.group(1), (i, m.group(2)))
    inside = {n for n, (i, _) in decl.items() if s <= i <= e}
    params = []
    for n, (i, typ) in decl.items():
        if n in inside or n == 'r':
            continue
        if re.search(r'(?<![\w.])' + re.escape(n) + r'(?!\w)', '\n'.join(clean)):
            params.append((n, typ, i))
            # an assignment to an outer local would be lost when it is passed by value
            if re.search(r'(?m)^\s*' + re.escape(n) + r'(\.\w+)*\s*(=|\+=)[^=]', '\n'.join(clean)):
                die('block assigns to outer local %r; resolve manually' % n)
    # other outer identifiers assigned in the block
    params.sort(key=lambda x: x[2])
    # a variable declared inside the block must not be used after it
    after = '\n'.join(strip_noise(x) for x in L[e + 1:fend])
    for n in inside:
        if re.search(r'(?<![\w.])' + re.escape(n) + r'(?!\w)', after):
            die('local %r declared in the block is used after it' % n)

    sig = ''.join(', %s %s' % (n, t) for n, t, _ in params)
    call = '\ts.catalog%sRoutes(r%s)' % (a.name, ''.join(', ' + n for n, _, _ in params))
    fname = 'catalog_%s.go' % a.name.lower()
    newpath = path.with_name(fname)
    if newpath.exists():
        die('%s already exists' % newpath)

    istart, iend, items = split_imports(L)
    # imports for the new file: those whose package name is used by block or by the signature
    need = []
    probe = '\n'.join(clean) + '\n' + sig + '\nchi.Router'
    for name, ipath, alias in items:
        if uses(name, probe):
            need.append((alias, ipath))
    # catalog.go keeps imports still used after the cut
    rest_lines = L[:s] + [call] + L[e + 1:]
    rest_body = '\n'.join(strip_noise(x) for x in rest_lines[iend + 1:])
    keep_lines = []
    for l in L[istart + 1:iend]:
        m = re.match(r'^\s*(?:(\w+)\s+)?"([^"]+)"\s*$', l)
        if not m:
            keep_lines.append(l)
            continue
        alias, ipath = m.group(1), m.group(2)
        name = import_name(alias, ipath)
        if uses(name, rest_body):
            keep_lines.append(l)
    # new file
    imp = ''.join('\t%s"%s"\n' % ((al + ' ') if al else '', p) for al, p in need)
    body = '\n'.join(block)
    newtext = 'package httpapi\n\nimport (\n%s)\n\nfunc (s *Server) catalog%sRoutes(r chi.Router%s) {\n%s\n}\n' % (imp, a.name, sig, body)
    print('block lines %d-%d (%d lines)' % (s + 1, e + 1, e - s + 1))
    print('new file   %s' % newpath)
    print('parameters %s' % (', '.join('%s %s' % (n, t) for n, t, _ in params) or '(none)'))
    print('call       %s' % call.strip())
    if a.dry_run:
        print('dry-run: nothing written')
        return
    newpath.write_text(newtext)
    out = L[:istart + 1] + keep_lines + L[iend:s] + [call] + L[e + 1:]
    path.write_text('\n'.join(out))
    print('done. Now run: go build ./... && go vet ./internal/httpapi && go test ./internal/httpapi -run TestRoutesGolden')


if __name__ == '__main__':
    main()
