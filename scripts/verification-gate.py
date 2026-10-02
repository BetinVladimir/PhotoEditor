#!/usr/bin/env python3
"""Fail-closed validator for verification that must run in an external environment."""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
GATES = {
    "native": "signed iOS and Android builds on configured SDK/toolchain hosts",
    "benchmark": "performance run on the requested production hardware profile",
    "restore": "backup restore drill in an isolated deployment",
    "visual": "licensed photo corpus and human acceptance review",
    "hardware": "model doctor and parity run on the target accelerator",
}


def commit() -> str:
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False,
    )
    return result.stdout.strip() if result.returncode == 0 else "unknown"


def emit(path: Path, body: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(body, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def validation_errors(body: dict[str, Any], gate: str, profile: str | None, source_commit: str) -> list[str]:
    required = {"gate", "status", "source_commit", "command", "environment", "exit_code", "artifacts", "metrics", "thresholds", "failed_cases", "skipped_count"}
    missing = sorted(required - body.keys())
    errors: list[str] = []
    if missing:
        errors.append("missing fields: " + ", ".join(missing))
    if body.get("gate") != gate:
        errors.append(f"gate must be {gate!r}")
    if body.get("status") != "PASS" or body.get("exit_code") != 0:
        errors.append("status must be PASS and exit_code must be 0")
    if body.get("source_commit") != source_commit:
        errors.append("source_commit does not match the current checkout")
    if profile and body.get("profile") != profile:
        errors.append(f"profile must be {profile!r}")
    if not isinstance(body.get("artifacts"), list) or not body.get("artifacts"):
        errors.append("artifacts must be a non-empty list")
    if not isinstance(body.get("metrics"), dict) or not body.get("metrics"):
        errors.append("metrics must be a non-empty object")
    if not isinstance(body.get("thresholds"), dict) or not body.get("thresholds"):
        errors.append("thresholds must be a non-empty object")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--gate", choices=sorted(GATES), required=True)
    parser.add_argument("--evidence", type=Path)
    parser.add_argument("--profile")
    args = parser.parse_args()
    output = ROOT / "work" / "evidence" / "gates" / f"{args.gate}.json"
    now = datetime.now(timezone.utc).isoformat()
    source_commit = commit()

    if args.evidence is None:
        body = {
            "gate": args.gate,
            "status": "blocked_external",
            "source_commit": source_commit,
            "recorded_at": now,
            "required_environment": GATES[args.gate],
            "profile": args.profile,
            "reason": "No external evidence file was supplied.",
        }
        emit(output, body)
        print(json.dumps(body, ensure_ascii=False))
        return 2

    evidence_path = args.evidence.expanduser().resolve()
    try:
        body = json.loads(evidence_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        print(f"invalid evidence {evidence_path}: {error}", file=sys.stderr)
        return 2
    errors = validation_errors(body, args.gate, args.profile, source_commit)
    if errors:
        rejected = {
            "gate": args.gate, "status": "rejected", "source_commit": source_commit,
            "recorded_at": now, "evidence": str(evidence_path), "errors": errors,
        }
        emit(output, rejected)
        print(json.dumps(rejected, ensure_ascii=False), file=sys.stderr)
        return 2
    accepted = dict(body)
    accepted["validated_at"] = now
    accepted["evidence"] = str(evidence_path)
    emit(output, accepted)
    print(json.dumps({"gate": args.gate, "status": "PASS", "evidence": str(evidence_path)}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
