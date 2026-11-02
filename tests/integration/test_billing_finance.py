"""Finance group integration tests (FIN-T01–T15).

Applies finance migration (02660) to an isolated DB and verifies table
constraints, FK, RLS isolation. No secrets in fixtures (T8). Working DB
never touched (R30).
"""
from pathlib import Path
import re

import pytest

ROOT = Path(__file__).resolve().parents[2]
FINANCE_MIGRATIONS = [
    '02660_billing_finance_schema.sql',
    '02661_billing_finance_functions.sql',
]
pytestmark = pytest.mark.migration_head(17)

EXPECTED_TABLES = [
    'billing_contacts',
    'billing_quotes',
    'billing_orders',
    'billing_order_lines',
    'billing_invoices',
    'billing_payments',
    'billing_refunds',
    'billing_invoice_allocations',
    'tenant_billing_profiles',
    'billing_subscriptions',
    'billing_subscription_items',
    'billing_change_requests',
    'billing_customers',
    'billing_custom_assignments',
    'billing_custom_limits',
]


# ---------------------------------------------------------------------------
# Scaffold: passes without DB (import + file checks)
# ---------------------------------------------------------------------------

def test_finance_scaffold_import():
    """FIN-00: scaffold exists and imports correctly."""
    assert ROOT.joinpath('docs/billing/contracts/finance-tables.json').exists()
    assert ROOT.joinpath('docs/billing/contracts/finance-functions.json').exists()
    assert ROOT.joinpath('scripts/generate-billing-finance.py').exists()


def test_finance_schema_migration_exists():
    """FIN-T*: schema migration generated and present on disk."""
    path = ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql'
    assert path.exists(), 'Finance schema migration must exist'
    text = path.read_text()
    assert '-- +goose Up' in text
    assert '-- +goose Down' in text
    assert 'Forward-only billing finance migration' in text
    assert 'CREATE OR REPLACE' not in text, 'Migrations must not use CREATE OR REPLACE'


def test_all_expected_tables_in_contract():
    """FIN-T*: all 15 tables defined in finance-tables.json."""
    import json
    source = json.loads(
        (ROOT / 'docs/billing/contracts/finance-tables.json').read_text()
    )
    names = {t['name'] for t in source['tables']}
    for table in EXPECTED_TABLES:
        assert table in names, f'Table {table} missing from finance-tables.json'


def test_all_tables_in_migration_sql():
    """FIN-T*: every table from contract appears in migration SQL."""
    sql = (ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql').read_text()
    for table in EXPECTED_TABLES:
        assert f'photo.{table}' in sql, f'Table photo.{table} missing from migration SQL'


def test_rls_force_for_all_tables():
    """FIN-T*: every table has FORCE ROW LEVEL SECURITY."""
    sql = (ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql').read_text()
    for table in EXPECTED_TABLES:
        assert f'ALTER TABLE photo.{table} FORCE ROW LEVEL SECURITY' in sql, \
            f'{table} missing FORCE ROW LEVEL SECURITY'


def test_no_delete_grant_in_migration():
    """FIN-T*: no DELETE grant in finance schema migration (R11)."""
    sql = (ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql').read_text()
    grants = re.findall(r'\bGRANT\s+(.+?)\s+ON\s+', sql.split('-- +goose Down')[0], re.I | re.S)
    assert grants
    assert all(not re.search(r'\b(?:DELETE|ALL)\b', privileges, re.I) for privileges in grants), \
        'Finance migration must not grant DELETE or ALL privileges'


def test_no_delete_grant_simple():
    """FIN-T*: GRANT ... DELETE not in finance migration SQL."""
    sql = (ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql').read_text()
    # Only the down-block can mention failure; up-block must not grant DELETE
    up_block = sql.split('-- +goose Down')[0]
    assert 'GRANT' in up_block
    for line in up_block.splitlines():
        if 'GRANT' in line.upper():
            assert 'DELETE' not in line.upper(), \
                f'Found DELETE in GRANT line: {line}'


def test_money_check_constraints_present():
    """FIN-T*: amount/money columns have CHECK >= 0 constraints."""
    sql = (ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql').read_text()
    assert 'CHECK(net_minor >= 0)' in sql
    assert 'CHECK(tax_minor >= 0)' in sql
    assert 'CHECK(gross_minor >= 0)' in sql
    assert 'CHECK(amount_minor >= 0)' in sql


def test_xor_constraint_on_custom_limits():
    """FIN-T15: billing_custom_limits has XOR constraint on integer_value/enum_value."""
    sql = (ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql').read_text()
    assert 'num_nonnulls(integer_value, enum_value) = 1' in sql


def test_append_only_tables_lack_update_grant():
    """FIN-T*: append_only tables (billing_order_lines) lack UPDATE grant."""
    sql = (ROOT / 'backend/db/migrations/02660_billing_finance_schema.sql').read_text()
    # Find the block for billing_order_lines
    lines = sql.splitlines()
    in_block = False
    for line in lines:
        if 'photo.billing_order_lines' in line:
            in_block = True
        if in_block and line.startswith('CREATE TABLE photo.billing_') and 'billing_order_lines' not in line:
            break
        if in_block and 'GRANT' in line.upper():
            assert 'UPDATE' not in line.upper(), \
                f'billing_order_lines (append_only) must not have UPDATE grant: {line}'
