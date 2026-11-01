"""TST-03: unit tests for docs/billing/tools/check-e2e-coverage.py.

Negative: exit 1 when an E-ID is missing, when docstring lacks "Положительный"
or "Отрицательный". Positive: exit 0 when all IDs are covered with correct docstrings.
Uses tmp_path — no DB, no network.
"""
from __future__ import annotations

import importlib.util
import io
import json
import textwrap
from contextlib import redirect_stdout
from pathlib import Path

import pytest

# ---------------------------------------------------------------------------
# Load the script as a module (dash in filename — importlib required)
# ---------------------------------------------------------------------------

_SCRIPT = Path(__file__).resolve().parents[2] / "docs/billing/tools/check-e2e-coverage.py"


def _load_cov():
    spec = importlib.util.spec_from_file_location("check_e2e_coverage", _SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


cov = _load_cov()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _write_backlog(d: Path, ids: list[int]) -> None:
    lines = [f"### E{n:02d} — scenario {n}\n\nsome text\n" for n in ids]
    (d / "BACKLOG-e2e-1.md").write_text("\n".join(lines), encoding="utf-8")


def _write_test(
    d: Path,
    sid: int,
    *,
    positive: bool = True,
    negative: bool = True,
) -> None:
    pos_line = "Положительный: something" if positive else "no pos here"
    neg_line = "Отрицательный: something" if negative else "no neg here"
    src = textwrap.dedent(f"""\
        def test_e{sid:02d}_scenario():
            \"\"\"
            {pos_line}
            {neg_line}
            \"\"\"
            assert True
    """)
    (d / f"test_billing_e2e_s{sid:02d}.py").write_text(src, encoding="utf-8")


# ---------------------------------------------------------------------------
# Negative: missing scenario
# ---------------------------------------------------------------------------

def test_missing_scenario_returns_exit_1(tmp_path):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1, 2])
    _write_test(tests_dir, 1)  # E02 not implemented
    rc = cov.main(["--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    assert rc == 1


def test_missing_scenario_reported_in_json(tmp_path):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1, 2])
    _write_test(tests_dir, 1)
    buf = io.StringIO()
    with redirect_stdout(buf):
        rc = cov.main(["--json", "--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    data = json.loads(buf.getvalue())
    assert rc == 1
    assert 2 in data["missing"]
    assert data["ok"] is False


# ---------------------------------------------------------------------------
# Negative: missing docstring keywords
# ---------------------------------------------------------------------------

def test_no_positive_in_docstring_returns_exit_1(tmp_path):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1])
    _write_test(tests_dir, 1, positive=False, negative=True)
    rc = cov.main(["--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    assert rc == 1


def test_no_negative_in_docstring_returns_exit_1(tmp_path):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1])
    _write_test(tests_dir, 1, positive=True, negative=False)
    rc = cov.main(["--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    assert rc == 1


def test_no_docstring_at_all_returns_exit_1(tmp_path):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1])
    (tests_dir / "test_billing_e2e_nodoc.py").write_text(
        "def test_e01_no_docstring():\n    assert True\n", encoding="utf-8"
    )
    rc = cov.main(["--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    assert rc == 1


# ---------------------------------------------------------------------------
# Negative: empty backlog directory
# ---------------------------------------------------------------------------

def test_empty_backlog_dir_returns_exit_1(tmp_path, capsys):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    rc = cov.main(["--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    assert rc == 1
    captured = capsys.readouterr()
    assert "ERROR" in captured.err


# ---------------------------------------------------------------------------
# Positive: all covered
# ---------------------------------------------------------------------------

def test_all_covered_returns_exit_0(tmp_path):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1, 2, 3])
    for i in [1, 2, 3]:
        _write_test(tests_dir, i)
    rc = cov.main(["--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    assert rc == 0


def test_all_covered_json_ok_true(tmp_path):
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1])
    _write_test(tests_dir, 1)
    buf = io.StringIO()
    with redirect_stdout(buf):
        rc = cov.main(["--json", "--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    data = json.loads(buf.getvalue())
    assert rc == 0
    assert data["ok"] is True
    assert data["missing"] == []


def test_extra_tests_not_in_backlog_still_passes(tmp_path):
    """Extra test_eNN_* not in backlog are reported but do not fail the check."""
    backlog_dir = tmp_path / "backlog"
    tests_dir = tmp_path / "tests"
    backlog_dir.mkdir()
    tests_dir.mkdir()
    _write_backlog(backlog_dir, [1])
    _write_test(tests_dir, 1)
    _write_test(tests_dir, 99)  # E99 not in backlog
    buf = io.StringIO()
    with redirect_stdout(buf):
        rc = cov.main(["--json", "--backlog-dir", str(backlog_dir), "--tests-dir", str(tests_dir)])
    data = json.loads(buf.getvalue())
    assert rc == 0
    assert 99 in data["extra_in_tests"]
