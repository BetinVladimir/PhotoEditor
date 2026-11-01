"""check-e2e-coverage.py — verify every E-ID from the e2e backlog has a test.

For each scenario defined in docs/billing/backlog/BACKLOG-e2e-*.md (E01–E90):
- A function matching test_eNN_* must exist in tests/integration/test_billing_e2e_*.py
- Its docstring must contain both "Положительный" and "Отрицательный"

Exit 0 if all scenarios are covered, 1 if any gap exists or on parse error.

Usage:
    python3 docs/billing/tools/check-e2e-coverage.py [--json] [--backlog-dir DIR] [--tests-dir DIR]
"""
from __future__ import annotations

import argparse
import ast
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def _collect_backlog_ids(backlog_dir: Path) -> list[int]:
    ids: set[int] = set()
    for f in sorted(backlog_dir.glob("BACKLOG-e2e-*.md")):
        for m in re.finditer(r"^### E(\d{2,3})\b", f.read_text(encoding="utf-8"), re.MULTILINE):
            ids.add(int(m.group(1)))
    return sorted(ids)


def _collect_test_functions(tests_dir: Path) -> dict[int, dict]:
    """Return {scenario_id: {file, name, has_positive, has_negative}}."""
    found: dict[int, dict] = {}
    for f in sorted(tests_dir.glob("test_billing_e2e_*.py")):
        try:
            tree = ast.parse(f.read_text(encoding="utf-8"), filename=str(f))
        except SyntaxError as exc:
            print(f"ERROR: cannot parse {f}: {exc}", file=sys.stderr)
            sys.exit(1)
        for node in ast.walk(tree):
            if not isinstance(node, ast.FunctionDef):
                continue
            m = re.match(r"test_e(\d{2,3})_", node.name)
            if not m:
                continue
            sid = int(m.group(1))
            docstring = ast.get_docstring(node) or ""
            rel = f.relative_to(ROOT) if ROOT in f.parents else f
            found[sid] = {
                "file": str(rel),
                "name": node.name,
                "has_positive": "Положительный" in docstring,
                "has_negative": "Отрицательный" in docstring,
            }
    return found


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--json", dest="as_json", action="store_true", help="output JSON")
    parser.add_argument("--backlog-dir", type=Path, default=ROOT / "docs/billing/backlog")
    parser.add_argument("--tests-dir", type=Path, default=ROOT / "tests/integration")
    args = parser.parse_args(argv)

    backlog_ids = _collect_backlog_ids(args.backlog_dir)
    if not backlog_ids:
        print("ERROR: no E-IDs found in backlog — check --backlog-dir", file=sys.stderr)
        return 1

    tests = _collect_test_functions(args.tests_dir)

    missing: list[int] = []
    no_positive: list[int] = []
    no_negative: list[int] = []

    for sid in backlog_ids:
        if sid not in tests:
            missing.append(sid)
        else:
            info = tests[sid]
            if not info["has_positive"]:
                no_positive.append(sid)
            if not info["has_negative"]:
                no_negative.append(sid)

    extra = sorted(set(tests) - set(backlog_ids))
    ok = not missing and not no_positive and not no_negative

    if args.as_json:
        print(json.dumps({
            "backlog_count": len(backlog_ids),
            "covered": len(backlog_ids) - len(missing),
            "missing": missing,
            "no_positive_docstring": no_positive,
            "no_negative_docstring": no_negative,
            "extra_in_tests": extra,
            "ok": ok,
        }, ensure_ascii=False, indent=2))
        return 0 if ok else 1

    print(f"Backlog scenarios : {len(backlog_ids)}")
    print(f"Covered           : {len(backlog_ids) - len(missing)}")
    if missing:
        print(f"  MISSING ({len(missing)}): " + ", ".join(f"E{n:02d}" for n in missing))
    if no_positive:
        print(f"  NO 'Положительный' in docstring ({len(no_positive)}): "
              + ", ".join(f"E{n:02d}" for n in no_positive))
    if no_negative:
        print(f"  NO 'Отрицательный' in docstring ({len(no_negative)}): "
              + ", ".join(f"E{n:02d}" for n in no_negative))
    if extra:
        print(f"  Extra (not in backlog): " + ", ".join(f"E{n:02d}" for n in extra))

    if ok:
        print("OK: all e2e scenarios covered")
    else:
        print("FAIL: e2e coverage incomplete")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
