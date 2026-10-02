from __future__ import annotations

import importlib.util
from pathlib import Path


def module():
    path = Path(__file__).parents[2] / "scripts" / "verification-gate.py"
    spec = importlib.util.spec_from_file_location("verification_gate", path)
    loaded = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(loaded)
    return loaded


def valid(**overrides):
    body = {
        "gate": "benchmark", "status": "PASS", "source_commit": "abc",
        "command": "runner", "environment": {"host": "target"}, "exit_code": 0,
        "artifacts": [{"sha256": "123"}], "metrics": {"p95": 1},
        "thresholds": {"p95": 2}, "failed_cases": [], "skipped_count": 0,
        "profile": "cpu45mp",
    }
    body.update(overrides)
    return body


def test_accepts_complete_current_commit_evidence():
    assert module().validation_errors(valid(), "benchmark", "cpu45mp", "abc") == []


def test_rejects_stale_wrong_profile_and_empty_measurements():
    errors = module().validation_errors(valid(source_commit="old", profile="gpu", metrics={}, artifacts=[]), "benchmark", "cpu45mp", "abc")
    assert any("source_commit" in value for value in errors)
    assert any("profile" in value for value in errors)
    assert any("artifacts" in value for value in errors)
    assert any("metrics" in value for value in errors)


def test_rejects_incomplete_or_nonpassing_evidence():
    body = valid(status="blocked_external", exit_code=2)
    del body["thresholds"]
    errors = module().validation_errors(body, "restore", None, "abc")
    assert any("missing fields" in value for value in errors)
    assert any("gate must be" in value for value in errors)
    assert any("status must be PASS" in value for value in errors)
    assert any("thresholds" in value for value in errors)
