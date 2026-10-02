from pathlib import Path


SQL = (Path(__file__).resolve().parents[2] / "backend/db/migrations/00155_apply_quality_gate.sql").read_text()


def test_apply_quality_is_a_sealed_canonical_epoch():
    assert "WHEN 'local' THEN 'quality'" in SQL
    assert "b.phase='quality'" in SQL
    assert "t.operation='image.local' AND t.state='succeeded'" in SQL
    assert "p_operation='image.local' AND p_role='working_image'" in SQL
    assert "CREATE FUNCTION photo.register_apply_quality_node" in SQL
    assert "a.produced_attempt=result.attempt" in SQL
    assert "a.produced_fence=result.fencing_token" in SQL
    assert "result.outputs @> jsonb_build_array" in SQL


def test_apply_adoption_requires_fenced_quality_report():
    assert "p_operation='quality.check' AND p_role='quality_report'" in SQL
    assert "qa.output_role='quality_report'" in SQL
    assert "qa.produced_attempt=qr.attempt" in SQL
    assert "qa.produced_fence=qr.fencing_token" in SQL
    assert ")<>b.item_count THEN RETURN false" in SQL
