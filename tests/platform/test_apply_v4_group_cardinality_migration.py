from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]

def test_group_cardinality_is_fenced_and_immutable():
    sql=(ROOT/'backend/db/migrations/00191_apply_v4_group_cardinality.sql').read_text()
    assert "cardinality IN('item','aggregate','group','review')" in sql
    assert 'set_apply_v4_group_count' in sql
    assert 's.expected_nodes IS NOT NULL AND s.expected_nodes<>p_count' in sql
    assert "WHEN 'group' THEN s.expected_nodes" in sql
    assert "p_node->>'operation'='image.outpaint'" in sql
    assert "RAISE EXCEPTION 'Forward-only migration'" in sql
