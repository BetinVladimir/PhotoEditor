from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00121_apply_output_quota.sql').read_text()

def test_apply_outputs_draw_from_the_run_reservation_once():
    assert 'ADD COLUMN run_reservation_id uuid' in SQL
    assert "x.holder_type='run' AND x.holder_id=t.run_id" in SQL
    assert "r.kind='apply'" in SQL
    assert 'SET bytes=bytes-p_bytes' in SQL
    assert 'IF NOT from_run THEN UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes' in SQL

def test_common_completion_and_cleanup_keep_owning_the_transferred_slice():
    complete=(ROOT/'backend/db/migrations/00021_gateway_outputs.sql').read_text()
    cleanup=(ROOT/'backend/db/migrations/00022_output_cleanup.sql').read_text()
    assert 'reserved_bytes=reserved_bytes-u.bytes,storage_bytes=storage_bytes+u.bytes' in complete
    assert 'reserved_bytes=reserved_bytes-u.bytes' in cleanup
