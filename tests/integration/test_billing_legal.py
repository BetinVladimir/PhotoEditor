"""Legal group integration tests (LEG-T01–T04).

Applies legal migration to an isolated DB and verifies table constraints,
FK, RLS isolation. No secrets in fixtures (T8). Working DB never touched (R30).
Tests are skipped until LEG-T* tasks add tables to the contracts.
"""
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
LEGAL_MIGRATIONS = [
    '02666_billing_legal_schema.sql',
    '02667_billing_legal_functions.sql',
]

EXPECTED_TABLES: list[str] = []  # populated by LEG-T01..T04


@pytest.fixture
def legal_db(billing_db):
    """Apply legal migrations on top of the billing fixture DB."""
    source = ROOT / 'docs/billing/contracts/legal-tables.json'
    import json
    catalog = json.loads(source.read_text())
    if not catalog['tables']:
        pytest.skip('No legal tables yet — skipping until LEG-T* tasks complete')
    for name in LEGAL_MIGRATIONS:
        path = ROOT / 'backend/db/migrations' / name
        if path.exists():
            billing_db.execute(path.read_text())
    return billing_db


@pytest.mark.skipif(not EXPECTED_TABLES, reason='LEG-T* tasks not yet complete')
def test_legal_tables_exist(legal_db):
    for table in EXPECTED_TABLES:
        result = legal_db.execute(
            "SELECT 1 FROM information_schema.tables "
            "WHERE table_schema='photo' AND table_name=$1",
            [table],
        ).fetchone()
        assert result is not None, f'Table photo.{table} not found'


@pytest.mark.skipif(not EXPECTED_TABLES, reason='LEG-T* tasks not yet complete')
def test_legal_rls_blocks_direct_access(legal_db):
    """Direct DML on legal tables must be blocked by RLS."""
    for table in EXPECTED_TABLES:
        with pytest.raises(Exception, match='new row violates|permission denied|RLS'):
            legal_db.execute(f'INSERT INTO photo.{table} DEFAULT VALUES')
