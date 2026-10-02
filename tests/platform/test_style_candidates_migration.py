from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00117_style_candidates.sql').read_text()


def test_candidate_claim_binds_canonical_aggregate_outputs():
    assert "t.operation='style.aggregate'" in SQL
    assert "t.state='succeeded'" in SQL and 't.canonical_result_id' in SQL
    assert "picked.outputs @> jsonb_build_array" in SQL
    assert "config.output_role='style_candidate'" in SQL
    assert "report.output_role='aggregate_report'" in SQL
    assert 'FOR UPDATE OF r SKIP LOCKED' in SQL


def test_candidate_finish_is_fenced_and_preserves_review_state():
    assert "c.materialize_token<>p_token" in SQL
    assert "c.materialize_lease_until<=clock_timestamp()" in SQL
    assert "p_config->>'schema_version'<>'2.0.0'" in SQL
    assert "p_config->>'renderer_version'<>'photo_renderer_v2'" in SQL
    assert "status='waiting_review'" in SQL


def test_scheduler_requires_candidate_functions():
    source=(ROOT/'backend/internal/workflow/service.go').read_text()
    assert "to_regprocedure('photo.claim_style_candidate()') IS NOT NULL" in source
    assert 'StyleCandidateMaterializer{' in source
    assert 'photo_scheduler_style_candidates_total' in source
