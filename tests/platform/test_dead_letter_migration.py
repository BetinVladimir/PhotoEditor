from pathlib import Path


SQL = (Path(__file__).resolve().parents[2] / "backend/db/migrations/00159_dead_letters.sql").read_text()


def test_dead_letters_never_persist_raw_payloads_and_replay_exact_task():
    assert "payload_sha256 text NOT NULL" in SQL
    assert "Raw message bytes" in SQL
    assert "p_metadata ?| ARRAY['payload','body','token','secret','authorization','cookie']" in SQL
    assert "t.state<>'failed'" in SQL
    assert "dispatch_command=NULL" in SQL
    assert "canonical_result_id IS NOT NULL" in SQL


def test_dead_letter_access_is_explicit_sysadmin_only():
    assert "photo.system_admins" in SQL
    assert "NOT EXISTS(SELECT FROM photo.system_admins" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.list_dead_letters" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.replay_dead_letter" in SQL
    assert "photo.authorize_system_admin" in SQL
    assert "s.revoked_at IS NULL" in SQL
    assert "s.expires_at>clock_timestamp()" in SQL
