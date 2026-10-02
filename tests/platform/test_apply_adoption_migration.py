from pathlib import Path
SQL=(Path(__file__).resolve().parents[2]/'backend/db/migrations/00123_adopt_apply_results.sql').read_text()

def test_adoption_requires_canonical_fenced_outputs():
    assert "t.state<>'succeeded' OR t.canonical_result_id IS NULL" in SQL
    assert 'x.produced_attempt=tr.attempt' in SQL
    assert 'x.produced_fence=tr.fencing_token' in SQL
    assert 'tr.outputs @>' in SQL
    assert "x.output_role='working_image'" in SQL

def test_adoption_creates_lineage_and_releases_only_the_unused_reservation():
    assert 'INSERT INTO photo.asset_versions' in SQL
    assert 'INSERT INTO photo.asset_version_sources' in SQL
    assert "'apply_source'" in SQL
    assert 'reserved_bytes=reserved_bytes-q.bytes' in SQL
    assert "phase='complete'" in SQL
    assert "status='completed'" in SQL
    assert 'finished_at' not in SQL
