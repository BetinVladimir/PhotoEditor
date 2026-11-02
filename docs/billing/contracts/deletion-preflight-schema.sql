CREATE TABLE photo.profile_deletion_preflights (
 preflight_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 privacy_case_id uuid NOT NULL UNIQUE REFERENCES photo.privacy_cases(case_id),
 user_id uuid NOT NULL REFERENCES photo.users(id),
 idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 128),
 auth_epoch bigint NOT NULL CHECK(auth_epoch>=0),
 subject_lifecycle_epoch bigint NOT NULL CHECK(subject_lifecycle_epoch>0),
 ownership_snapshot jsonb NOT NULL CHECK(jsonb_typeof(ownership_snapshot)='array'),
 ownership_snapshot_hash bytea NOT NULL CHECK(octet_length(ownership_snapshot_hash)=32),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(user_id,idempotency_key)
);
ALTER TABLE photo.users ADD COLUMN privacy_lifecycle_epoch bigint NOT NULL DEFAULT 1 CHECK(privacy_lifecycle_epoch>0);
REVOKE ALL ON photo.profile_deletion_preflights FROM PUBLIC;
ALTER TABLE photo.profile_deletion_preflights ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.profile_deletion_preflights FORCE ROW LEVEL SECURITY;
CREATE POLICY deletion_preflight_functions ON photo.profile_deletion_preflights TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.profile_deletion_preflights TO photo_fn_owner;
