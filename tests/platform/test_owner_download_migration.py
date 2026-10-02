from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SQL = (ROOT / "backend/db/migrations/00099_owner_downloads.sql").read_text()

def test_redeem_supports_guest_and_user_with_current_policy_checks():
    for required in (
        "CREATE FUNCTION photo.redeem_download_intent",
        "i.principal_type NOT IN ('guest','user')",
        "IF i.principal_type='guest'",
        "l.permissions->'download'<>'true'::jsonb",
        "u.status='active'",
        "t.status='active'",
        "x.download_allowed",
        "p.visibility='restricted'",
        "photo.project_acl",
        "ci.asset_version_id=si.asset_version_id",
        "pub.state='published'",
        "UPDATE photo.download_intents SET consumed_at=clock_timestamp()",
        "x.object_version IS NOT NULL",
        "GRANT EXECUTE ON FUNCTION photo.redeem_download_intent(bytea,text) TO photo_api,photo_portal",
    ):
        assert required in SQL
