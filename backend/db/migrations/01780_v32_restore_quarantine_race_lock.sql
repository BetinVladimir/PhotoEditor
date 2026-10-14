-- +goose Up
-- Serialize publication and quarantine admission by the global physical object identity.
-- Collisions in the advisory hash only serialize unrelated objects; they cannot admit one.
CREATE INDEX artifacts_ready_physical_quarantine_idx
 ON photo.artifacts(bucket,object_key,object_version) WHERE state='ready';
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.refuse_restore_quarantined_artifact() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.state='ready' THEN
  IF current_setting('transaction_isolation')<>'read committed' THEN
   RAISE EXCEPTION 'RESTORE_QUARANTINE_REQUIRES_READ_COMMITTED' USING ERRCODE='23514';
  END IF;
  PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(
   pg_catalog.jsonb_build_array(NEW.bucket,NEW.object_key,NEW.object_version)::text,0));
  IF EXISTS(SELECT FROM photo.restore_quarantine_versions q
   WHERE q.bucket=NEW.bucket AND q.object_key=NEW.object_key AND q.object_version=NEW.object_version) THEN
   RAISE EXCEPTION 'RESTORE_OBJECT_QUARANTINED' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.refuse_restore_quarantined_artifact() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.refuse_restore_quarantined_artifact() FROM PUBLIC,
 photo_api,photo_worker,photo_scheduler,photo_gateway,photo_maintenance,photo_portal,photo_publisher,photo_auth;
CREATE FUNCTION photo.refuse_restore_referenced_quarantine() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'RESTORE_QUARANTINE_REQUIRES_READ_COMMITTED' USING ERRCODE='23514';
 END IF;
 PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(
  pg_catalog.jsonb_build_array(NEW.bucket,NEW.object_key,NEW.object_version)::text,0));
 IF EXISTS(SELECT FROM photo.artifacts a WHERE a.state='ready'
  AND a.bucket=NEW.bucket AND a.object_key=NEW.object_key AND a.object_version=NEW.object_version) THEN
  RAISE EXCEPTION 'RESTORE_QUARANTINE_REFERENCED_OBJECT' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.refuse_restore_referenced_quarantine() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.refuse_restore_referenced_quarantine() FROM PUBLIC,
 photo_api,photo_worker,photo_scheduler,photo_gateway,photo_maintenance,photo_portal,photo_publisher,photo_auth;
CREATE TRIGGER refuse_restore_referenced_quarantine
 BEFORE INSERT OR UPDATE OF bucket,object_key,object_version ON photo.restore_quarantine_versions
 FOR EACH ROW EXECUTE FUNCTION photo.refuse_restore_referenced_quarantine();
CREATE FUNCTION photo.refuse_restore_quarantine_mutation() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RAISE EXCEPTION 'RESTORE_QUARANTINE_IMMUTABLE' USING ERRCODE='23514';
END $$;
ALTER FUNCTION photo.refuse_restore_quarantine_mutation() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.refuse_restore_quarantine_mutation() FROM PUBLIC,
 photo_api,photo_worker,photo_scheduler,photo_gateway,photo_maintenance,photo_portal,photo_publisher,photo_auth;
CREATE TRIGGER refuse_restore_quarantine_mutation
 BEFORE UPDATE OR DELETE ON photo.restore_quarantine_versions
 FOR EACH ROW EXECUTE FUNCTION photo.refuse_restore_quarantine_mutation();
CREATE TRIGGER refuse_restore_quarantine_mutation
 BEFORE UPDATE OR DELETE ON photo.restore_quarantine_runs
 FOR EACH ROW EXECUTE FUNCTION photo.refuse_restore_quarantine_mutation();
CREATE TRIGGER refuse_restore_quarantine_truncate
 BEFORE TRUNCATE ON photo.restore_quarantine_versions
 FOR EACH STATEMENT EXECUTE FUNCTION photo.refuse_restore_quarantine_mutation();
CREATE TRIGGER refuse_restore_quarantine_truncate
 BEFORE TRUNCATE ON photo.restore_quarantine_runs
 FOR EACH STATEMENT EXECUTE FUNCTION photo.refuse_restore_quarantine_mutation();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
