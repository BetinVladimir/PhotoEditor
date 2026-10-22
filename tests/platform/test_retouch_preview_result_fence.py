"""Static regression expectations; not a substitute for transactional SQL scenarios."""
from pathlib import Path


def test_retouch_commit_wraps_existing_chain_and_checks_complete_bundle():
    root=Path(__file__).resolve().parents[2]
    sql=(root/'backend/db/migrations/02363_v27_retouch_preview_bundle_commit.sql').read_text()
    assert 'RENAME TO commit_task_success_before_retouch_bundle' in sql
    assert 'SELECT * INTO committed FROM photo.commit_task_success_before_retouch_bundle' in sql
    assert sql.count('photo.semantic_retouch_preview_task_live(p_task)')==2
    assert "IF guarded AND committed.outcome='committed' THEN" in sql
    assert 'photo.semantic_retouch_preview_bundle_ready(p_task,committed.result_id,p_attempt,p_fence)' in sql
    assert "AND a.state='ready')=8" in sql
    assert "t.operation='semantic.preview_retouch_region'" in sql
    assert "t.resource_class='cpu'" in sql
    assert 'photo.lock_semantic_decision_publication' in sql
    assert 'FOR UPDATE' in sql and 'Forward-only migration' in sql
    for role in ('working_image','pixels','preview_evidence','preview_presentation','before','after','affected','protected'):
        assert "'"+role+"'" in sql
