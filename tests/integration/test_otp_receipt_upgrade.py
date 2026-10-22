"""Forward OTP migration preserves legacy challenges and runtime privileges."""
from pathlib import Path
from uuid import uuid4
import pytest

ROOT=Path(__file__).resolve().parents[2]

@pytest.mark.migration_head(2532)
def test_otp_receipt_upgrade_preserves_legacy_challenges_and_acl(db):
    challenge=uuid4()
    db.execute("""INSERT INTO photo.auth_challenges(id,email_normalized,code_hmac,
      expires_at,locale,request_ip_hash) VALUES(%s,%s,%s,
      clock_timestamp()+interval '5 minutes','en',%s)""",
      (challenge,str(uuid4())+'@otp-upgrade.test',bytes(32),bytes(32)))
    before=db.execute('SELECT to_jsonb(c) FROM photo.auth_challenges c WHERE id=%s',(challenge,)).fetchone()[0]
    acl=db.execute("SELECT relowner,relacl::text FROM pg_class WHERE oid='photo.auth_challenges'::regclass").fetchone()
    migration=(ROOT/'backend/db/migrations/02533_otp_verify_encrypted_receipt.sql').read_text().split('-- +goose Down')[0]
    db.execute(migration)
    after=db.execute('SELECT to_jsonb(c) FROM photo.auth_challenges c WHERE id=%s',(challenge,)).fetchone()[0]
    for column in ('verify_receipt_binding','verify_receipt_cipher','verify_receipt_expires_at','verify_receipt_epoch'):
        assert after.pop(column) is None
        assert db.execute('SELECT has_column_privilege(%s,%s,%s,%s)',('photo_auth','photo.auth_challenges',column,'UPDATE')).fetchone()==(True,)
    assert after==before
    assert db.execute("SELECT relowner,relacl::text FROM pg_class WHERE oid='photo.auth_challenges'::regclass").fetchone()==acl
    assert db.execute("SELECT convalidated FROM pg_constraint WHERE conrelid='photo.auth_challenges'::regclass AND conname='auth_verify_receipt_complete'").fetchone()==(True,)
    # Existing rows continue to support the legacy single-use lifecycle.
    db.execute('SET LOCAL ROLE photo_auth')
    db.execute('UPDATE photo.auth_challenges SET consumed_at=clock_timestamp() WHERE id=%s',(challenge,))
    assert db.execute('SELECT consumed_at IS NOT NULL,verify_receipt_cipher IS NULL FROM photo.auth_challenges WHERE id=%s',(challenge,)).fetchone()==(True,True)
