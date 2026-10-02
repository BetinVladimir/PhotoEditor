from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00171_style_analysis_runtime_selection.sql').read_text()
SERVICE=(ROOT/'backend/internal/style/service.go').read_text()
SCHEDULER=(ROOT/'backend/internal/workflow/service.go').read_text()


def test_style_runtime_selection_is_tenant_bound_and_private():
    assert "SECURITY DEFINER" in SQL
    assert "p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid" in SQL
    assert "REVOKE ALL ON FUNCTION photo.select_style_analysis_runtimes" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.select_style_analysis_runtimes(uuid,boolean) TO photo_api" in SQL
    assert "FROM photo.select_style_analysis_runtimes($1,$2)" in SERVICE
    assert "FROM photo.worker_registrations w WHERE w.tenant_id=$1" not in SERVICE


def test_style_build_constructor_validates_immutable_run_and_forces_fks():
    for token in ("r.kind='analyze'","NOT r.graph_sealed","r.config_snapshot->>'style_id'=p_style::text","SET CONSTRAINTS ALL IMMEDIATE"):
        assert token in SQL
    assert "GRANT SELECT,INSERT,UPDATE,DELETE ON photo.style_analysis_builds,photo.style_analysis_nodes TO photo_fn_owner" in SQL
    assert "photo.create_style_analysis_build($1,$2,$3,$4)" in SERVICE


def test_scheduler_preflight_requires_current_style_output_signature():
    assert "photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text,text)" in SCHEDULER
    assert "photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text)'" not in SCHEDULER
