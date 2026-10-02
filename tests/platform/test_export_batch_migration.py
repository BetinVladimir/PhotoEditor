from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00162_export_batches.sql').read_text()

def test_export_batch_pins_children_and_finalizes_parent():
    for token in ('export_batches','export_batch_children','preset_revision','preset_hash','finalize_export_batch','FOR UPDATE SKIP LOCKED'):
        assert token in SQL

def test_export_batch_tables_are_tenant_isolated():
    assert SQL.count('FORCE ROW LEVEL SECURITY') >= 2
    assert SQL.count('CREATE POLICY tenant_scope') >= 2
