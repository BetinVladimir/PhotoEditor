from pathlib import Path


def test_media_retry_intake_is_outbox_fenced_and_replay_safe():
    root=Path(__file__).resolve().parents[2]
    sql=(root/'backend/db/migrations/00149_media_retry_intake.sql').read_text()
    assert "CREATE TABLE photo.media_retry_jobs" in sql
    assert "CREATE FUNCTION photo.accept_media_retry_command" in sql
    assert "o.event_id=p_event" in sql and "o.routing_key='media.retry'" in sql
    assert "si.item_id=p_item AND si.asset_version_id=p_version" in sql
    assert "r.status='succeeded'" in sql
    assert "UNIQUE(tenant_id,bulk_operation_id,item_id)" in sql
    assert "GRANT EXECUTE ON FUNCTION photo.accept_media_retry_command" in sql
