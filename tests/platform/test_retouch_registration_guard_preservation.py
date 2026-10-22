"""Shared SQL guard compatibility with all existing workflow/Apply scopes."""
from pathlib import Path


def test_retouch_scope_extension_preserves_existing_guard_body():
    root=Path(__file__).resolve().parents[2]/'backend/db/migrations'
    previous=(root/'02328_v27_shown_background_cleanup_input_sealing.sql').read_text()
    current=(root/'02354_v27_retouch_preview_task_registration.sql').read_text()
    def body(sql):
        start=sql.index('CREATE OR REPLACE FUNCTION photo.guard_v2_artifact_link')
        return sql[start:sql.index('-- +goose StatementEnd',start)].strip()
    extra='    OR photo.semantic_retouch_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)\n'
    assert body(current).count(extra)==1
    assert body(current).replace(extra,'')==body(previous)
