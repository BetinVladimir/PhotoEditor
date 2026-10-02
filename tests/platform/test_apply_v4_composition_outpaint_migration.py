from pathlib import Path


ROOT=Path(__file__).resolve().parents[2]


def test_composition_outpaint_choice_is_closed_and_forward_only():
    sql=(ROOT/'backend/db/migrations/00188_apply_v4_composition_outpaint.sql').read_text()
    assert "s.phase='composition_render'" in sql
    assert "s.operation='geometry.crop'" in sql
    assert "p_node->>'operation'='image.outpaint'" in sql
    assert "'geometry.crop','image.outpaint'" in sql
    assert "n.phase=s.phase" in sql
    assert "RAISE EXCEPTION 'Forward-only migration'" in sql
