from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00163_interactive_tasks.sql').read_text()
def test_interactive_tasks_are_fenced_version_pinned_real_tasks():
    for token in ('FORCE ROW LEVEL SECURITY','claim_interactive_task','plan_interactive_params','reserve_interactive_document','materialize_interactive_task','operation_registry','object_version IS NOT NULL'):
        assert token in SQL
def test_interactive_task_kinds_are_closed():
    assert "kind IN('mask_manual','mask_prompt','generation')" in SQL
    assert "WHEN 'mask_manual' THEN 'mask.rasterize'" in SQL
