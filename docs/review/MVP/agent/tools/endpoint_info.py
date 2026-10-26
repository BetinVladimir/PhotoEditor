#!/usr/bin/env python3
"""Print the contract of one HTTP endpoint with its request/response type definitions.

Usage (repo root):  python3 docs/review/MVP/agent/tools/endpoint_info.py POST '/api/v1/tenants/{t}/projects'
Sources: packages/contracts/endpoints.json, docs/implementation-v2/contracts/schemas.json,
Go `type X struct`, TypeScript `interface X` / `type X`. Read-only. Used by recon cards (T-2*).
"""
import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[5]
MAX_STRUCT_LINES = 60


def die(msg):
    print(msg, file=sys.stderr)
    sys.exit(2)


def go_struct(name):
    try:
        out = subprocess.run(['grep', '-rn', '--include=*.go', '--exclude=*_test.go', '-E',
                              r'^type %s (struct|interface)' % re.escape(name), str(ROOT / 'backend')],
                             capture_output=True, text=True).stdout.strip().splitlines()
    except Exception:
        return None
    if not out:
        return None
    path, line, _ = out[0].split(':', 2)
    lines = pathlib.Path(path).read_text().split('\n')
    start = int(line) - 1
    end = start
    depth = 0
    for i in range(start, min(len(lines), start + MAX_STRUCT_LINES)):
        depth += lines[i].count('{') - lines[i].count('}')
        end = i
        if depth <= 0 and i > start or (depth == 0 and '{' in lines[i] and '}' in lines[i]):
            break
    return '%s:%d\n%s' % (pathlib.Path(path).relative_to(ROOT), start + 1, '\n'.join(lines[start:end + 1]))


def ts_type(name):
    for base in ('packages/api-client/src', 'apps/photographer/src', 'apps/portal/src'):
        try:
            out = subprocess.run(['grep', '-rn', '--include=*.ts', '--include=*.tsx', '--exclude=*.test.*', '-E',
                                  r'^export (interface|type) %s\b' % re.escape(name), str(ROOT / base)],
                                 capture_output=True, text=True).stdout.strip().splitlines()
        except Exception:
            continue
        if out:
            path, line, _ = out[0].split(':', 2)
            lines = pathlib.Path(path).read_text().split('\n')
            start = int(line) - 1
            end = min(len(lines), start + 40)
            return '%s:%d\n%s' % (pathlib.Path(path).relative_to(ROOT), start + 1, '\n'.join(lines[start:end]))
    return None


def handler_snippet(method, path):
    """First lines of the Go route handler, to see which struct the body is decoded into."""
    verb = method.capitalize()
    pat = r'r\.%s\(\s*"%s"' % (verb, re.escape(path))
    try:
        out = subprocess.run(['grep', '-rn', '--include=*.go', '--exclude=*_test.go', '-E', pat,
                              str(ROOT / 'backend/internal/httpapi')], capture_output=True, text=True).stdout.strip().splitlines()
    except Exception:
        return None
    if not out:
        return None
    f, line, _ = out[0].split(':', 2)
    lines = pathlib.Path(f).read_text().split('\n')
    s = int(line) - 1
    return '%s:%d\n%s' % (pathlib.Path(f).relative_to(ROOT), s + 1, '\n'.join(l[:160] for l in lines[s:s + 14]))


def schema(name, schemas):
    node = schemas.get(name)
    if node is None and isinstance(schemas.get('schemas'), dict):
        node = schemas['schemas'].get(name)
    if node is None and isinstance(schemas.get('$defs'), dict):
        node = schemas['$defs'].get(name)
    if node is None:
        return None
    return json.dumps(node, ensure_ascii=False)[:1800]


def main():
    if len(sys.argv) != 3:
        die(__doc__)
    method, path = sys.argv[1].upper(), sys.argv[2]
    rows = json.loads((ROOT / 'packages/contracts/endpoints.json').read_text())['endpoints']
    row = next((r for r in rows if r['method'] == method and r['path'] == path), None)
    if row is None:
        near = sorted({r['path'] for r in rows if path.split('{')[0] in r['path']})[:12]
        die('no such endpoint %s %s. Similar paths: %s' % (method, path, near))
    t = row.get('typed_source_contract') or {}
    print('%s %s' % (method, path))
    for k in ('handler', 'role', 'success_status', 'idempotency', 'revision_required', 'description'):
        if row.get(k) not in (None, ''):
            print('  %s: %s' % (k, row[k]))
    schemas = {}
    sp = ROOT / 'docs/implementation-v2/contracts/schemas.json'
    if sp.exists():
        schemas = json.loads(sp.read_text())
    names = [('request', row.get('request_schema') or t.get('request')), ('response', row.get('response_schema') or t.get('response'))]
    for label, name in names:
        if not name or name in ('Empty', 'None'):
            print('  %s: (empty)' % label)
            continue
        print('\n== %s type: %s' % (label, name))
        body = schema(name, schemas) or go_struct(name) or ts_type(name)
        print(body if body else 'UNKNOWN: definition of %s not found; see the handler snippet below' % name)
    snip = handler_snippet(method, path)
    if snip:
        print('\n== handler snippet (Go)\n' + snip)
        m = re.search(r'var \w+ (\w+)\.(\w+)\b', snip)
        if m:
            pkg, typ = m.groups()
            for cand in (ROOT / 'backend/internal' / pkg, ROOT / 'backend/internal' / (pkg.replace('service', ''))):
                if cand.is_dir():
                    hit = subprocess.run(['grep', '-rn', '--include=*.go', '--exclude=*_test.go', '-E',
                                          r'^type %s struct' % typ, str(cand)], capture_output=True, text=True).stdout.strip().splitlines()
                    if hit:
                        path_, line_, _ = hit[0].split(':', 2)
                        ls = pathlib.Path(path_).read_text().split('\n')
                        st = int(line_) - 1
                        en = next((i for i in range(st, min(len(ls), st + 60)) if ls[i].startswith('}')), st + 30)
                        print('\n== decoded body type %s.%s\n%s:%d\n%s' % (pkg, typ, pathlib.Path(path_).relative_to(ROOT), st + 1, '\n'.join(ls[st:en + 1])))
                        break


if __name__ == '__main__':
    main()
