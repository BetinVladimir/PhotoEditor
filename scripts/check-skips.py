#!/usr/bin/env python3
"""Fail when tests contain skips outside the reviewed baseline (T-1: skip = fail).

Usage: check-skips.py [--update]   (--update rewrites scripts/skips-baseline.txt)
A skip is recorded as `path: <skip marker>` with a count, so line shifts do not
cause churn, but any new skip in a file fails CI.
"""
import collections, pathlib, re, sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
BASELINE = ROOT / 'scripts' / 'skips-baseline.txt'
PATTERN = re.compile(r'pytest\.skip\(|pytest\.mark\.skip(?:if)?\b|unittest\.skip|\b(?:it|test|describe)\.skip\b|\bt\.Skip(?:Now|f)?\(|\b(?:xit|xdescribe)\(')
GLOBS = ['tests/**/*.py', 'workers/tests/**/*.py', 'apps/*/src/**/*.test.ts*', 'apps/*/app/**/*.test.ts*', 'packages/**/*.test.ts*', 'backend/**/*_test.go']
SKIP_DIRS = {'node_modules', '.venv', '.venv-platform', '.next', 'work', '.cache', 'vendor', 'dist', '_work'}


def scan():
    found = collections.Counter()
    for pattern in GLOBS:
        for path in ROOT.glob(pattern):
            if SKIP_DIRS & set(path.parts):
                continue
            try:
                text = path.read_text(encoding='utf-8', errors='replace')
            except OSError:
                continue
            n = len(PATTERN.findall(text))
            if n:
                found[str(path.relative_to(ROOT))] = n
    return found


def main():
    found = scan()
    if '--update' in sys.argv:
        BASELINE.write_text(''.join(f'{p}\t{n}\n' for p, n in sorted(found.items())))
        print(f'baseline updated: {sum(found.values())} skips in {len(found)} files')
        return 0
    base = {}
    if BASELINE.exists():
        for line in BASELINE.read_text().splitlines():
            path, _, n = line.partition('\t')
            base[path] = int(n)
    bad = [(p, n, base.get(p, 0)) for p, n in sorted(found.items()) if n > base.get(p, 0)]
    for p, n, b in bad:
        print(f'NEW SKIP: {p}: {n} skip markers (baseline {b})')
    if bad:
        print('Skipped tests are failures. Fix the test or review and run --update.')
        return 1
    print(f'ok: {sum(found.values())} baseline skips, none new')
    return 0


if __name__ == '__main__':
    sys.exit(main())
