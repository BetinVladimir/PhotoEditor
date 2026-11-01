"""record-evidence.py — append an acceptance evidence block to STATUS.md.

Reads the stdout of pytest or go test from a file, extracts passed/failed/skipped
counts, and appends a formatted block under the matching ACC-* section in STATUS.md.

Usage:
    python3 docs/billing/tools/record-evidence.py <ACC-id> <output-file>

Example:
    python3 docs/billing/tools/record-evidence.py ACC-01 /tmp/pytest-out.txt

Exit codes:
    0  block appended
    1  parse error or bad arguments
"""
from __future__ import annotations

import re
import subprocess
import sys
from datetime import date
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
STATUS_MD = ROOT / "docs/billing/STATUS.md"


# ---------------------------------------------------------------------------
# Parse test output
# ---------------------------------------------------------------------------

def _parse_counts(text: str) -> dict[str, int] | None:
    """Return {passed, failed, skipped} or None if unrecognisable."""
    # pytest summary line: "12 passed, 2 skipped, 1 failed in 10.2s"
    pytest_m = re.search(
        r"(\d+) passed(?:,\s*(\d+) skipped)?(?:,\s*(\d+) failed)?",
        text,
    )
    if pytest_m:
        return {
            "passed": int(pytest_m.group(1)),
            "skipped": int(pytest_m.group(2) or 0),
            "failed": int(pytest_m.group(3) or 0),
        }

    # go test summary: "ok  \tbackend/internal/billing\t0.123s"
    # or "FAIL\tbackend/internal/billing\t0.123s"
    go_ok = re.findall(r"^ok\s+\S+", text, re.MULTILINE)
    go_fail = re.findall(r"^FAIL\s+\S+", text, re.MULTILINE)
    if go_ok or go_fail:
        return {
            "passed": len(go_ok),
            "skipped": 0,
            "failed": len(go_fail),
        }

    return None


def _acceptance_verdict(counts: dict[str, int]) -> str:
    if counts["failed"] > 0:
        return "приёмка не пройдена (failed>0)"
    if counts["skipped"] > 0:
        return "приёмка не пройдена (skipped>0)"
    return "приёмка пройдена"


# ---------------------------------------------------------------------------
# Git rev
# ---------------------------------------------------------------------------

def _git_rev() -> str:
    try:
        return subprocess.check_output(
            ["git", "rev-parse", "--short", "HEAD"],
            cwd=ROOT,
            stderr=subprocess.DEVNULL,
        ).decode().strip()
    except Exception:
        return "unknown"


# ---------------------------------------------------------------------------
# Build and append block
# ---------------------------------------------------------------------------

def _build_block(acc_id: str, counts: dict[str, int], command_hint: str) -> str:
    verdict = _acceptance_verdict(counts)
    today = date.today().isoformat()
    rev = _git_rev()
    result_line = (
        f"passed={counts['passed']} "
        f"failed={counts['failed']} "
        f"skipped={counts['skipped']}"
    )
    return (
        f"\n### {acc_id} — {today} — {rev}\n"
        f"- Команда: {command_hint}\n"
        f"- Результат: {result_line}\n"
        f"- Вердикт: {verdict}\n"
    )


def _append_to_status(block: str, status_path: Path) -> None:
    with open(status_path, "a", encoding="utf-8") as f:
        f.write(block)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if len(args) != 2:
        print(f"Usage: {Path(sys.argv[0]).name} <ACC-id> <output-file>", file=sys.stderr)
        return 1

    acc_id, output_file_str = args[0], args[1]
    if not re.fullmatch(r"[A-Z]+-\d+", acc_id):
        print(f"ERROR: acc_id must match [A-Z]+-<number>, got: {acc_id!r}", file=sys.stderr)
        return 1

    output_path = Path(output_file_str)
    if not output_path.exists():
        print(f"ERROR: file not found: {output_path}", file=sys.stderr)
        return 1

    text = output_path.read_text(encoding="utf-8", errors="replace")
    counts = _parse_counts(text)
    if counts is None:
        print(
            "ERROR: could not parse passed/failed/skipped from output.\n"
            "Expected pytest summary (e.g. '12 passed, 2 skipped in 5.0s') "
            "or go test summary lines (ok/FAIL + package).",
            file=sys.stderr,
        )
        return 1

    block = _build_block(acc_id, counts, command_hint=str(output_path))
    _append_to_status(block, STATUS_MD)

    verdict = _acceptance_verdict(counts)
    print(f"Appended {acc_id} to STATUS.md — {verdict}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
