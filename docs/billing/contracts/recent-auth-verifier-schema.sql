CREATE TABLE photo.account_recent_auth_challenges (
 challenge_id uuid PRIMARY KEY REFERENCES photo.auth_challenges(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES photo.users(id),
 session_id uuid NOT NULL REFERENCES photo.sessions(id),
 auth_epoch bigint NOT NULL CHECK(auth_epoch>=0),
 purpose text NOT NULL CHECK(purpose IN ('delete_profile','resolve_ownership')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 grant_id uuid UNIQUE REFERENCES photo.account_recent_auth_grants(grant_id),
 verified_at timestamptz,
 receipt_binding bytea,
 receipt_cipher bytea,
 receipt_expires_at timestamptz,
 CHECK((grant_id IS NULL AND verified_at IS NULL) OR (grant_id IS NOT NULL AND verified_at IS NOT NULL)),
 CHECK((receipt_binding IS NULL AND receipt_cipher IS NULL AND receipt_expires_at IS NULL)
 OR (grant_id IS NOT NULL AND receipt_binding IS NOT NULL AND octet_length(receipt_binding)=32
 AND receipt_cipher IS NOT NULL AND octet_length(receipt_cipher) BETWEEN 29 AND 32768 AND receipt_expires_at IS NOT NULL))
);
REVOKE ALL ON photo.account_recent_auth_challenges FROM PUBLIC;
ALTER TABLE photo.account_recent_auth_challenges ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.account_recent_auth_challenges FORCE ROW LEVEL SECURITY;
CREATE POLICY recent_auth_functions ON photo.account_recent_auth_challenges TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.account_recent_auth_challenges TO photo_fn_owner;
