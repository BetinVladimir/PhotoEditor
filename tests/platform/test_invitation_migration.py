from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SQL = (ROOT / "backend/db/migrations/00098_invitations.sql").read_text()


def test_invitation_create_is_atomic_idempotent_and_mails_only_ciphertext():
    for required in (
        "CREATE UNIQUE INDEX invitations_token_hash_uq",
        "CREATE FUNCTION photo.create_invitation",
        "SECURITY DEFINER SET search_path=pg_catalog,photo",
        "photo.authorize_tenant(p_user,p_session,p_tenant)",
        "a.role IN ('owner','admin')",
        "INSERT INTO photo.idempotency_records",
        "IF idem.request_hash<>p_request_hash",
        "INSERT INTO photo.invitations",
        "INSERT INTO photo.mail_payloads",
        "INSERT INTO photo.auth_mail_outbox",
        "'invitation.create'",
    ):
        assert required in SQL


def test_invitation_accept_binds_live_session_email_token_and_active_tenant():
    for required in (
        "CREATE FUNCTION photo.accept_invitation",
        "s.revoked_at IS NULL AND s.expires_at>clock_timestamp()",
        "x.token_hash=p_token_hash FOR UPDATE",
        "i.email_normalized<>u.email_normalized",
        "i.expires_at<=clock_timestamp()",
        "t.status='active' FOR UPDATE",
        "IF i.accepted_at IS NOT NULL",
        "INSERT INTO photo.memberships",
        "UPDATE photo.invitations SET accepted_at=clock_timestamp()",
        "'invitation.accept'",
    ):
        assert required in SQL


def test_invitation_functions_are_private_capabilities():
    assert "REVOKE ALL ON FUNCTION photo.create_invitation" in SQL
    assert "REVOKE ALL ON FUNCTION photo.accept_invitation" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.create_invitation" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.accept_invitation" in SQL
