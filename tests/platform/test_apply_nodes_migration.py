from pathlib import Path
SQL=(Path(__file__).resolve().parents[2]/'backend/db/migrations/00122_apply_nodes.sql').read_text()

def test_apply_nodes_are_immutable_staged_and_fenced():
    assert 'CREATE TABLE photo.apply_nodes' in SQL
    assert "('mask','stats','resolve','color','local')" in SQL
    assert 'photo.register_apply_node' in SQL
    assert 'photo.materialize_apply_task' in SQL
    assert 'b.lease_token<>p_token' in SQL
    assert "t.state='succeeded' AND t.canonical_result_id IS NOT NULL" in SQL
    assert 'tr.outputs @>' in SQL
    assert 'a.created_by_task_id=t.id' in SQL
    assert 'a.produced_attempt=tr.attempt' in SQL
    assert 'a.produced_fence=tr.fencing_token' in SQL

def test_apply_params_have_a_narrow_document_authority():
    assert 'photo.reserve_apply_document' in SQL
    assert 'photo.complete_apply_document' in SQL
    assert "d.owner_id=n.id AND d.purpose='task_params'" in SQL

def test_new_canonical_output_queries_use_the_real_artifact_columns():
    root=Path(__file__).resolve().parents[2]
    sources='\n'.join((root/'backend/db/migrations'/name).read_text() for name in (
        '00119_style_analysis_nodes.sql','00122_apply_nodes.sql','00123_adopt_apply_results.sql'))
    assert 'producer_task_id' not in sources
    assert 'producer_attempt' not in sources
    assert 'producer_fencing_token' not in sources
    assert 'created_by_task_id' in sources
    assert 'produced_attempt' in sources
    assert 'produced_fence' in sources
