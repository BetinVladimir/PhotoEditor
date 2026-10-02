from pathlib import Path


SQL = (Path(__file__).resolve().parents[2] / "backend/db/migrations/00157_portal_item_download_intents.sql").read_text()


def test_item_intent_pins_publication_preset_and_export():
    assert "intent_kind='item'" in SQL
    assert "c.state='published'" in SQL
    assert "x.visibility='visible'" in SQL
    assert "x.moderation_state='accepted'" in SQL
    assert "x.review_state='accepted'" in SQL
    assert "x.preset_hash=pr.config_hash" in SQL
    assert "x.status='ready'" in SQL
    assert "x.artifact_id=i.artifact_id" in SQL


def test_item_redeem_rechecks_current_share_policy_and_consumes_once():
    assert "l.policy_version<>i.policy_version" in SQL
    assert "l.permissions->'download'<>'true'::jsonb" in SQL
    assert "jsonb_array_elements(l.allowed_presets)" in SQL
    assert "i.consumed_at IS NOT NULL" in SQL
    assert "SET consumed_at=clock_timestamp()" in SQL

