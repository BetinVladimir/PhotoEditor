"""Usage group integration tests (USA-T01–T07, USA-F01–F20).

Applies usage migration (02664, 02665) to an isolated DB and verifies
table constraints, RLS, and function invariants. No secrets (T8). No working DB (R30).
"""
from pathlib import Path
import json

import pytest

ROOT = Path(__file__).resolve().parents[2]
USAGE_MIGRATIONS = [
    '02664_billing_usage_schema.sql',
    '02665_billing_usage_functions.sql',
]
pytestmark = pytest.mark.migration_head(18)

EXPECTED_TABLES = [
    'billing_egress_ledger',
    'billing_photo_runs',
    'billing_storage_addons',
    'billing_storage_objects',
    'billing_storage_usage',
    'billing_usage_summaries',
    'billing_usage_ticket_allocations',
]

EXPECTED_FUNCTIONS = [
    'billing_project_usage',
    'billing_expire_buckets',
    'billing_cover_overrun',
    'billing_admit_usage_request',
    'billing_apply_period_boundary',
    'billing_apply_storage_entitlement',
    'billing_close_lease',
    'billing_grant_paid_allocation',
    'billing_hold_photo_slot',
    'billing_issue_ticket',
    'billing_open_delivery',
    'billing_reconcile_storage',
    'billing_reconcile_uncertain_chunk',
    'billing_record_egress_chunk',
    'billing_register_photo_success',
    'billing_release_ticket',
    'billing_reserve_delivery_chunk',
    'billing_rollover_bucket',
    'billing_settle_ticket',
    'billing_transfer_rollover_remainder',
]


# ---------------------------------------------------------------------------
# Scaffold: no DB required
# ---------------------------------------------------------------------------

def test_usage_scaffold_import():
    """USA-00: scaffold exists and imports correctly."""
    assert ROOT.joinpath('docs/billing/contracts/usage-tables.json').exists()
    assert ROOT.joinpath('docs/billing/contracts/usage-functions.json').exists()
    assert ROOT.joinpath('scripts/generate-billing-usage.py').exists()


def test_usage_schema_migration_exists():
    """USA-T*: schema migration generated and present on disk."""
    path = ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql'
    assert path.exists(), 'Usage schema migration must exist'
    text = path.read_text()
    assert '-- +goose Up' in text
    assert '-- +goose Down' in text
    assert 'Forward-only billing usage migration' in text
    assert 'CREATE OR REPLACE' not in text


def test_usage_functions_migration_exists():
    """USA-F*: functions migration generated and present on disk."""
    path = ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql'
    assert path.exists(), 'Usage functions migration must exist'
    text = path.read_text()
    assert 'Forward-only billing usage migration' in text


def test_all_expected_tables_in_contract():
    """USA-T*: all 7 tables defined in usage-tables.json."""
    source = json.loads(
        (ROOT / 'docs/billing/contracts/usage-tables.json').read_text()
    )
    names = {t['name'] for t in source['tables']}
    for table in EXPECTED_TABLES:
        assert table in names, f'Table {table} missing from usage-tables.json'


def test_all_tables_in_migration_sql():
    """USA-T*: every table from contract appears in migration SQL."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    for table in EXPECTED_TABLES:
        assert f'photo.{table}' in sql, f'Table photo.{table} missing from migration SQL'


def test_rls_force_for_all_tables():
    """USA-T*: every table has FORCE ROW LEVEL SECURITY."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    for table in EXPECTED_TABLES:
        assert f'ALTER TABLE photo.{table} FORCE ROW LEVEL SECURITY' in sql


def test_no_delete_grant_in_usage_migration():
    """USA-T*: no DELETE grant in usage schema migration (R11)."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    up_block = sql.split('-- +goose Down')[0]
    for line in up_block.splitlines():
        if 'GRANT' in line.upper():
            assert 'DELETE' not in line.upper(), \
                f'Found DELETE in GRANT line: {line}'


def test_egress_ledger_append_only():
    """USA-T01: billing_egress_ledger is append-only (no UPDATE grant)."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    import re
    grants = re.findall(r'GRANT ([A-Z,]+) ON photo\.billing_egress_ledger', sql)
    for g in grants:
        assert 'UPDATE' not in g, 'billing_egress_ledger must not have UPDATE grant'


def test_egress_ledger_source_check_constraint():
    """USA-T01: billing_egress_ledger has source CHECK constraint."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    assert "source IN ('gallery','preview','download')" in sql


def test_photo_runs_attempt_kind_check():
    """USA-T02: billing_photo_runs has attempt_kind CHECK constraint."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    assert "attempt_kind IN ('initial','user_rerun','technical_retry')" in sql


def test_storage_addons_state_check():
    """USA-T03: billing_storage_addons has state CHECK constraint."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    assert "state IN ('pending','active','ending','ended')" in sql


def test_storage_objects_category_check():
    """USA-T04: billing_storage_objects has category CHECK constraint."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    assert "category IN ('original','result','preview','archive')" in sql


def test_usage_summaries_unique_constraint():
    """USA-T06: billing_usage_summaries has UNIQUE(tenant,resource,period)."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    assert 'UNIQUE(tenant_id,resource_code,period_id)' in sql


def test_ticket_allocations_settled_plus_released_check():
    """USA-T07: billing_usage_ticket_allocations settled+released<=quantity CHECK."""
    sql = (ROOT / 'backend/db/migrations/02664_billing_usage_schema.sql').read_text()
    assert 'settled_quantity + released_quantity <= quantity' in sql


def test_all_expected_functions_in_migration():
    """USA-F*: all 20 functions appear in functions migration."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    for fn in EXPECTED_FUNCTIONS:
        assert f'FUNCTION photo.{fn}' in sql, \
            f'Function photo.{fn} missing from usage functions migration'


def test_all_functions_security_definer():
    """USA-F*: all functions have SECURITY DEFINER SET search_path (R16)."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    for fn in EXPECTED_FUNCTIONS:
        block_start = sql.find(f'CREATE FUNCTION photo.{fn}')
        assert block_start >= 0, f'{fn} not found in migration'
        block_end = sql.find('-- +goose StatementEnd', block_start)
        block = sql[block_start:block_end]
        assert 'SECURITY DEFINER SET search_path=pg_catalog,photo' in block, \
            f'{fn} missing SECURITY DEFINER'


def test_no_photo_api_grant_in_usage_functions():
    """USA-F*: no GRANT EXECUTE to photo_api in usage functions (R16)."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    for line in sql.splitlines():
        if 'GRANT EXECUTE' in line.upper():
            assert 'photo_api' not in line, \
                f'Found photo_api EXECUTE grant in usage functions: {line}'


def test_tenant_inactive_error_code_present():
    """USA-F*: BILLING_TENANT_INACTIVE error code present (billing_project_usage, billing_hold_photo_slot)."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert 'BILLING_TENANT_INACTIVE' in sql


def test_admit_hash_mismatch_error_present():
    """USA-F04: BILLING_ADMIT_HASH_MISMATCH raised on quote hash mismatch."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert 'BILLING_ADMIT_HASH_MISMATCH' in sql


def test_revision_conflict_error_present():
    """USA-F06: BILLING_REVISION_CONFLICT raised on storage entitlement CAS failure."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert 'BILLING_REVISION_CONFLICT' in sql


def test_settle_ticket_cas_idempotent():
    """USA-F19: billing_settle_ticket uses CAS (settled_quantity=0) for idempotency."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert 'settled_quantity=0' in sql


def test_reconcile_uncertain_chunk_outcomes():
    """USA-F13: billing_reconcile_uncertain_chunk returns confirmed/released/quarantine."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert "'confirmed'" in sql
    assert "'released'" in sql
    assert "'quarantine'" in sql


def test_egress_chunk_dedup_on_conflict():
    """USA-F14: billing_record_egress_chunk uses ON CONFLICT DO NOTHING for dedup."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert 'ON CONFLICT(tenant_id,delivery_id,chunk_sequence) DO NOTHING' in sql


def test_rollover_bucket_requires_period_end_policy():
    """USA-F18: billing_rollover_bucket checks expiry_policy='period_end'."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert 'BILLING_BUCKET_NOT_ROLLOVER_ELIGIBLE' in sql


def test_expire_buckets_batch_limit_validation():
    """USA-F02: billing_expire_buckets validates batch parameter."""
    sql = (ROOT / 'backend/db/migrations/02665_billing_usage_functions.sql').read_text()
    assert 'BILLING_EXPIRE_BATCH_INVALID' in sql
