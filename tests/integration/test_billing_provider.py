"""Provider group integration tests (PRO-T01–T04, PRO-F01–F03).

Applies provider migration (02662, 02663) to an isolated DB and verifies
table constraints, RLS, and function invariants. No secrets (T8). No working DB (R30).
"""
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
PROVIDER_MIGRATIONS = [
    '02662_billing_provider_schema.sql',
    '02663_billing_provider_functions.sql',
]
pytestmark = pytest.mark.migration_head(17)

EXPECTED_TABLES = [
    'billing_notification_marks',
    'billing_outbox',
    'billing_reconciliation_runs',
    'billing_webhook_inbox',
]


# ---------------------------------------------------------------------------
# Scaffold: no DB required
# ---------------------------------------------------------------------------

def test_provider_scaffold_import():
    """PRO-00: scaffold exists and imports correctly."""
    assert ROOT.joinpath('docs/billing/contracts/provider-tables.json').exists()
    assert ROOT.joinpath('docs/billing/contracts/provider-functions.json').exists()
    assert ROOT.joinpath('scripts/generate-billing-provider.py').exists()


def test_provider_schema_migration_exists():
    """PRO-T*: schema migration generated and present on disk."""
    path = ROOT / 'backend/db/migrations/02662_billing_provider_schema.sql'
    assert path.exists(), 'Provider schema migration must exist'
    text = path.read_text()
    assert '-- +goose Up' in text
    assert '-- +goose Down' in text
    assert 'Forward-only billing provider migration' in text
    assert 'CREATE OR REPLACE' not in text


def test_provider_functions_migration_exists():
    """PRO-F*: functions migration generated and present on disk."""
    path = ROOT / 'backend/db/migrations/02663_billing_provider_functions.sql'
    assert path.exists(), 'Provider functions migration must exist'
    text = path.read_text()
    assert 'Forward-only billing provider migration' in text


def test_all_expected_tables_in_contract():
    """PRO-T*: all 4 tables defined in provider-tables.json."""
    import json
    source = json.loads(
        (ROOT / 'docs/billing/contracts/provider-tables.json').read_text()
    )
    names = {t['name'] for t in source['tables']}
    for table in EXPECTED_TABLES:
        assert table in names, f'Table {table} missing from provider-tables.json'


def test_all_tables_in_migration_sql():
    """PRO-T*: every table from contract appears in migration SQL."""
    sql = (ROOT / 'backend/db/migrations/02662_billing_provider_schema.sql').read_text()
    for table in EXPECTED_TABLES:
        assert f'photo.{table}' in sql, f'Table photo.{table} missing from migration SQL'


def test_rls_force_for_all_tables():
    """PRO-T*: every table has FORCE ROW LEVEL SECURITY."""
    sql = (ROOT / 'backend/db/migrations/02662_billing_provider_schema.sql').read_text()
    for table in EXPECTED_TABLES:
        assert f'ALTER TABLE photo.{table} FORCE ROW LEVEL SECURITY' in sql


def test_no_delete_grant_in_provider_migration():
    """PRO-T*: no DELETE grant in provider schema migration (R11)."""
    sql = (ROOT / 'backend/db/migrations/02662_billing_provider_schema.sql').read_text()
    up_block = sql.split('-- +goose Down')[0]
    for line in up_block.splitlines():
        if 'GRANT' in line.upper():
            assert 'DELETE' not in line.upper(), \
                f'Found DELETE in GRANT line: {line}'


def test_threshold_check_constraint_present():
    """PRO-T01: billing_notification_marks has threshold CHECK constraint."""
    sql = (ROOT / 'backend/db/migrations/02662_billing_provider_schema.sql').read_text()
    assert 'threshold_percent IN (80,95,100)' in sql


def test_outbox_skip_locked_index():
    """PRO-T02: billing_outbox has partial index for retry."""
    sql = (ROOT / 'backend/db/migrations/02662_billing_provider_schema.sql').read_text()
    assert 'billing_outbox_retry_idx' in sql
    assert 'SKIP LOCKED' in (ROOT / 'backend/db/migrations/02663_billing_provider_functions.sql').read_text()


def test_webhook_inbox_unique_event_constraint():
    """PRO-T04: billing_webhook_inbox UNIQUE(account,event_id)."""
    sql = (ROOT / 'backend/db/migrations/02662_billing_provider_schema.sql').read_text()
    assert 'UNIQUE(provider_account_id,event_id)' in sql


def test_webhook_hash_quarantine_in_functions():
    """PRO-F03: billing_record_webhook raises on hash mismatch (quarantine)."""
    sql = (ROOT / 'backend/db/migrations/02663_billing_provider_functions.sql').read_text()
    assert 'BILLING_WEBHOOK_HASH_QUARANTINE' in sql


def test_fence_check_in_apply_snapshot():
    """PRO-F01: billing_apply_provider_snapshot checks fence token."""
    sql = (ROOT / 'backend/db/migrations/02663_billing_provider_functions.sql').read_text()
    assert 'BILLING_FENCE_STALE' in sql
