from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]

def test_apply_v4_reads_version_pinned_sensor_metadata():
    sql=(ROOT/'backend/db/migrations/00190_apply_v4_sensor_metadata.sql').read_text()
    assert 'asset_metadata jsonb' in sql
    assert 'v.metadata' in sql
    assert 'v.id=i.asset_version_id' in sql
    assert "RAISE EXCEPTION 'Forward-only migration'" in sql
