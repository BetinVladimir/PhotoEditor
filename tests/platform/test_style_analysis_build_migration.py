from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SQL = (ROOT / "backend/db/migrations/00118_style_analysis_builds.sql").read_text()
NODES = (ROOT / "backend/db/migrations/00119_style_analysis_nodes.sql").read_text()


def test_analysis_build_is_fenced_and_stage_gated():
    assert "phase IN ('registration','color_fit','aggregate','complete','failed')" in SQL
    assert "FOR UPDATE OF b SKIP LOCKED" in SQL
    assert "lease_token=p_token" in SQL
    assert "lease_until>=clock_timestamp()" in SQL
    assert "t.operation='pair.register'" in SQL
    assert "t.operation='pair.fit.color'" in SQL


def test_analysis_graph_uses_sealed_dispatch_epochs():
    assert "SET graph_sealed=false" in SQL
    assert "graph_sealed=true" in SQL
    assert "UPDATE photo.tasks SET state='ready'" in SQL
    assert "status='failed',graph_sealed=true" in SQL


def test_analyze_api_freezes_versioned_inputs():
    source = (ROOT / "backend/internal/style/service.go").read_text()
    assert "ba.object_version IS NOT NULL" in source
    assert "aa.object_version IS NOT NULL" in source
    assert "canonical.Hash(pairs)" in source
    assert "'style-analysis/v1'" not in source  # Go literal uses double quotes.
    assert '"style-analysis/v1"' in source
    routes = (ROOT / "backend/internal/httpapi/catalog.go").read_text()
    assert 'styles/{id}/analyses' in routes


def test_analysis_nodes_bind_plans_documents_and_canonical_outputs():
    assert "CREATE TABLE photo.style_analysis_nodes" in NODES
    assert "register_style_analysis_node" in NODES
    assert "reserve_style_analysis_document" in NODES
    assert "complete_style_analysis_document" in NODES
    assert "materialize_style_analysis_task" in NODES
    assert "tr.outputs @> jsonb_build_array" in NODES
    assert "t.canonical_result_id IS NOT NULL" in NODES
    assert "a.object_version IS NOT NULL" in NODES


def test_scheduler_requires_and_runs_style_analysis_coordinator():
    source = (ROOT / "backend/internal/workflow/service.go").read_text()
    assert "to_regprocedure('photo.claim_style_analysis_build()') IS NOT NULL" in source
    assert "StyleAnalysisCoordinator{" in source
    assert "photo_scheduler_style_analysis_transitions_total" in source
