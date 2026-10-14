-- +goose Up
-- Operator-only physical-version quarantine. No application table grants.
CREATE TABLE photo.restore_quarantine_runs (
 id uuid PRIMARY KEY,plan_sha256 text NOT NULL CHECK(plan_sha256 ~ '^[0-9a-f]{64}$'),
 extra_count bigint NOT NULL CHECK(extra_count>=0),extra_bytes bigint NOT NULL CHECK(extra_bytes>=0),
 activated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE photo.restore_quarantine_versions (
 run_id uuid NOT NULL REFERENCES photo.restore_quarantine_runs(id),
 bucket text NOT NULL CHECK(octet_length(bucket) BETWEEN 1 AND 63),
 object_key text NOT NULL CHECK(octet_length(object_key) BETWEEN 1 AND 1024),
 object_version text NOT NULL CHECK(octet_length(object_version) BETWEEN 1 AND 1024 AND object_version<>'null'),
 bytes bigint NOT NULL CHECK(bytes>=0),
 PRIMARY KEY(run_id,bucket,object_key,object_version)
);
CREATE INDEX restore_quarantine_physical ON photo.restore_quarantine_versions(bucket,object_key,object_version);
ALTER TABLE photo.restore_quarantine_runs OWNER TO photo_fn_owner;
ALTER TABLE photo.restore_quarantine_versions OWNER TO photo_fn_owner;
REVOKE ALL ON photo.restore_quarantine_runs,photo.restore_quarantine_versions FROM PUBLIC,
 photo_api,photo_worker,photo_scheduler,photo_gateway,photo_maintenance,photo_portal,photo_publisher,photo_auth;
-- +goose StatementBegin
CREATE FUNCTION photo.refuse_restore_quarantined_artifact() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.state='ready' AND EXISTS(SELECT FROM photo.restore_quarantine_versions q
  WHERE q.bucket=NEW.bucket AND q.object_key=NEW.object_key AND q.object_version=NEW.object_version) THEN
  RAISE EXCEPTION 'RESTORE_OBJECT_QUARANTINED' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.refuse_restore_quarantined_artifact() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.refuse_restore_quarantined_artifact() FROM PUBLIC;
CREATE TRIGGER refuse_restore_quarantined_artifact
 BEFORE INSERT OR UPDATE OF state,bucket,object_key,object_version ON photo.artifacts
 FOR EACH ROW EXECUTE FUNCTION photo.refuse_restore_quarantined_artifact();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
