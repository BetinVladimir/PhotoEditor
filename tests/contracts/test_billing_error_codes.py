"""Contract test: all http-class billing error codes are handled in MapError (T-02).

Invariants:
- Every code in error-classes.json with class=="http" must appear in MapError.
- Every code mapped in MapError must exist in at least one billing migration.
- A new RAISE EXCEPTION without an error-classes.json entry → "БЕЗ РЕШЕНИЯ" section must be empty.
"""
import json, re, glob
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DOCS = ROOT / "docs" / "billing"
MIGRATIONS = ROOT / "backend" / "db" / "migrations"
WORKFLOW = ROOT / "backend" / "internal" / "billing" / "workflow.go"
CLASSES = DOCS / "contracts" / "error-classes.json"


def _sql_codes() -> set[str]:
    pattern = MIGRATIONS / "02[56][0-9][0-9]_*billing*.sql"
    sql = "".join(p.read_text(encoding="utf-8") for p in sorted(MIGRATIONS.glob("02[56][0-9][0-9]_*billing*.sql")))
    return set(re.findall(r"RAISE EXCEPTION '((?:BILLING|PHOTO_INITIAL)[A-Z0-9_]*)'", sql))


def _go_mapped() -> dict[str, tuple[str, int]]:
    body = WORKFLOW.read_text(encoding="utf-8")
    start = body.index("func MapError")
    body = body[start:]
    mapped: dict[str, tuple[str, int]] = {}
    for m in re.finditer(
        r'case ([^:]+):\s*return domain\.NewError\("([A-Z_]+)", (\d+)\)', body
    ):
        for c in re.findall(r'"([A-Z_]+)"', m.group(1)):
            mapped[c] = (m.group(2), int(m.group(3)))
    # also handle lock-timeout via pg.Code == "55P03"
    return mapped


def _decisions() -> dict:
    return json.loads(CLASSES.read_text(encoding="utf-8"))["decisions"]


def test_all_http_class_codes_are_mapped_in_go():
    """Every code with class=='http' in error-classes.json must be in MapError."""
    dec = _decisions()
    mapped = _go_mapped()
    missing = [
        code for code, v in dec.items()
        if v["class"] == "http" and code not in mapped
    ]
    assert not missing, f"http-class codes not yet in MapError: {missing}"


def test_mapped_codes_match_declared_response():
    """For every http code in error-classes.json, MapError returns the declared code+status."""
    dec = _decisions()
    mapped = _go_mapped()
    mismatches = []
    for sql_code, v in dec.items():
        if v["class"] != "http":
            continue
        if sql_code not in mapped:
            continue  # caught by previous test
        go_code, go_status = mapped[sql_code]
        if go_code != v["code"] or go_status != v["status"]:
            mismatches.append(
                f"{sql_code}: want {v['code']} {v['status']}, got {go_code} {go_status}"
            )
    assert not mismatches, "MapError response mismatch:\n" + "\n".join(mismatches)


def test_mapped_codes_exist_in_migrations():
    """Every SQL code handled in MapError must appear in a billing migration."""
    sql_codes = _sql_codes()
    mapped = _go_mapped()
    # Exclude the lock-code "55P03" and non-BILLING codes that are intentionally generic
    generic = {"BILLING_UNAVAILABLE"}
    phantom = [c for c in mapped if c not in sql_codes and c not in generic]
    assert not phantom, f"MapError handles codes absent from billing migrations: {phantom}"


# Baseline of codes without a decision (R7: owner must decide, not the model).
# If this number grows, a new RAISE EXCEPTION was added without classification → FAIL.
# To raise the limit: add a decision to error-classes.json first, then update this constant.
_UNDECIDED_LIMIT = 53


def test_no_new_undecided_billing_codes():
    """A new RAISE EXCEPTION without a record in error-classes.json AND without a MapError branch fails."""
    sql_codes = _sql_codes()
    dec = _decisions()
    mapped = _go_mapped()
    undecided = sorted(c for c in sql_codes if c not in dec and c not in mapped)
    assert len(undecided) <= _UNDECIDED_LIMIT, (
        f"Undecided billing codes grew from {_UNDECIDED_LIMIT} to {len(undecided)}. "
        "Add new codes to docs/billing/contracts/error-classes.json first:\n"
        + "\n".join(undecided[_UNDECIDED_LIMIT:])
    )
