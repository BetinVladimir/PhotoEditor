"""TST-05: unit tests for docs/billing/tools/record-evidence.py.

Covers three output formats: clean pass, with skips, with failures.
No DB, no network, no real STATUS.md modified (uses tmp_path).
"""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

# ---------------------------------------------------------------------------
# Load the script as a module (dash in filename — importlib required)
# ---------------------------------------------------------------------------

_SCRIPT = Path(__file__).resolve().parents[2] / "docs/billing/tools/record-evidence.py"


def _load_rec():
    spec = importlib.util.spec_from_file_location("record_evidence", _SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


rec = _load_rec()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _write_output(tmp_path: Path, content: str) -> Path:
    p = tmp_path / "pytest-out.txt"
    p.write_text(content, encoding="utf-8")
    return p


def _run(acc_id: str, output_file: Path, status_path: Path) -> int:
    """Run main() redirecting STATUS_MD to a tmp file."""
    original = rec.STATUS_MD
    rec.STATUS_MD = status_path
    try:
        return rec.main([acc_id, str(output_file)])
    finally:
        rec.STATUS_MD = original


# ---------------------------------------------------------------------------
# _parse_counts — unit tests for the parser
# ---------------------------------------------------------------------------

class TestParseCounts:
    def test_clean_pytest(self):
        counts = rec._parse_counts("10 passed in 5.0s")
        assert counts == {"passed": 10, "failed": 0, "skipped": 0}

    def test_pytest_with_skipped(self):
        counts = rec._parse_counts("12 passed, 2 skipped in 8.1s")
        assert counts is not None
        assert counts["passed"] == 12
        assert counts["skipped"] == 2
        assert counts["failed"] == 0

    def test_pytest_with_failed(self):
        counts = rec._parse_counts("8 passed, 1 failed in 4.3s")
        assert counts is not None
        assert counts["passed"] == 8
        assert counts["failed"] == 1
        assert counts["skipped"] == 0

    def test_go_test_ok(self):
        text = "ok  \tbackend/internal/billing\t0.123s\nok  \tbackend/internal/run\t0.456s\n"
        counts = rec._parse_counts(text)
        assert counts is not None
        assert counts["passed"] == 2
        assert counts["failed"] == 0

    def test_go_test_fail(self):
        text = "ok  \tbackend/internal/billing\t0.1s\nFAIL\tbackend/internal/run\t0.2s\n"
        counts = rec._parse_counts(text)
        assert counts is not None
        assert counts["passed"] == 1
        assert counts["failed"] == 1

    def test_unrecognisable_returns_none(self):
        assert rec._parse_counts("some random output with no counts") is None


# ---------------------------------------------------------------------------
# _acceptance_verdict
# ---------------------------------------------------------------------------

class TestVerdict:
    def test_clean_pass(self):
        assert rec._acceptance_verdict({"passed": 10, "failed": 0, "skipped": 0}) == "приёмка пройдена"

    def test_skipped(self):
        v = rec._acceptance_verdict({"passed": 10, "failed": 0, "skipped": 2})
        assert "skipped>0" in v
        assert "не пройдена" in v

    def test_failed(self):
        v = rec._acceptance_verdict({"passed": 5, "failed": 1, "skipped": 0})
        assert "failed>0" in v
        assert "не пройдена" in v


# ---------------------------------------------------------------------------
# main() integration: each call appends, previous blocks are never overwritten (T6)
# ---------------------------------------------------------------------------

def test_appends_block_to_status(tmp_path):
    output = _write_output(tmp_path, "10 passed in 5.0s")
    status = tmp_path / "STATUS.md"
    status.write_text("# STATUS\n", encoding="utf-8")
    rc = _run("ACC-01", output, status)
    assert rc == 0
    content = status.read_text(encoding="utf-8")
    assert "### ACC-01" in content
    assert "passed=10" in content
    assert "failed=0" in content
    assert "skipped=0" in content
    assert "приёмка пройдена" in content


def test_skipped_output_marks_not_accepted(tmp_path):
    output = _write_output(tmp_path, "12 passed, 2 skipped in 8.1s")
    status = tmp_path / "STATUS.md"
    status.write_text("# STATUS\n", encoding="utf-8")
    rc = _run("ACC-02", output, status)
    assert rc == 0
    content = status.read_text(encoding="utf-8")
    assert "skipped=2" in content
    assert "не пройдена" in content


def test_failed_output_marks_not_accepted(tmp_path):
    output = _write_output(tmp_path, "8 passed, 1 failed in 4.3s")
    status = tmp_path / "STATUS.md"
    status.write_text("# STATUS\n", encoding="utf-8")
    rc = _run("ACC-03", output, status)
    assert rc == 0
    content = status.read_text(encoding="utf-8")
    assert "failed=1" in content
    assert "не пройдена" in content


def test_repeated_calls_do_not_overwrite_old_blocks(tmp_path):
    """Two separate runs append two separate blocks (T6 — counts never summed)."""
    output1 = _write_output(tmp_path, "10 passed in 5.0s")
    status = tmp_path / "STATUS.md"
    status.write_text("# STATUS\n", encoding="utf-8")
    _run("ACC-01", output1, status)

    output2 = tmp_path / "pytest-out2.txt"
    output2.write_text("15 passed in 7.0s", encoding="utf-8")
    _run("ACC-01", output2, status)

    content = status.read_text(encoding="utf-8")
    blocks = [line for line in content.splitlines() if line.startswith("### ACC-01")]
    assert len(blocks) == 2, "Both blocks must exist; neither was overwritten"


def test_bad_acc_id_returns_1(tmp_path):
    output = _write_output(tmp_path, "10 passed in 5.0s")
    status = tmp_path / "STATUS.md"
    status.write_text("# STATUS\n", encoding="utf-8")
    rc = _run("not-an-id", output, status)
    assert rc == 1


def test_missing_output_file_returns_1(tmp_path, capsys):
    status = tmp_path / "STATUS.md"
    status.write_text("# STATUS\n", encoding="utf-8")
    rc = _run("ACC-01", tmp_path / "nonexistent.txt", status)
    assert rc == 1
    assert "not found" in capsys.readouterr().err


def test_unrecognisable_output_returns_1(tmp_path, capsys):
    output = _write_output(tmp_path, "some random text with no counts")
    status = tmp_path / "STATUS.md"
    status.write_text("# STATUS\n", encoding="utf-8")
    rc = _run("ACC-01", output, status)
    assert rc == 1
    assert "ERROR" in capsys.readouterr().err
