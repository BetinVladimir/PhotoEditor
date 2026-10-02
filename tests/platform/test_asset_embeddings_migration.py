from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00161_asset_embeddings.sql').read_text()

def test_embedding_projection_is_fenced_and_bound_to_canonical_output():
    for token in ('FORCE ROW LEVEL SECURITY','claim_asset_embedding','finish_asset_embedding','canonical_result_id','scene_embedding','cardinality(p_values)<>384'):
        assert token in SQL

def test_embedding_projection_rejects_nonfinite_and_nonunit_vectors():
    assert "'NaN','Infinity','-Infinity'" in SQL
    assert 'n NOT BETWEEN .999 AND 1.001' in SQL
