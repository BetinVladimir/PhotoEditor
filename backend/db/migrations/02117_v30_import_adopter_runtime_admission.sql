-- +goose Up
-- A new import run needs both the continuation scheduler and the independent
-- result adopter. Existing draft replay is outside this readiness predicate.
CREATE TABLE photo.v30_import_adopter_runtime_leases (
 instance_id uuid PRIMARY KEY,
 expires_at timestamptz NOT NULL CHECK (isfinite(expires_at))
);
ALTER TABLE photo.v30_import_adopter_runtime_leases OWNER TO photo_fn_owner;
ALTER TABLE photo.v30_import_adopter_runtime_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_import_adopter_runtime_leases FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_import_adopter_runtime_leases
 TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.v30_import_adopter_runtime_leases FROM PUBLIC,photo_api,
 photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT SELECT,INSERT,UPDATE ON photo.v30_import_adopter_runtime_leases
 TO photo_fn_owner;
CREATE INDEX v30_import_adopter_runtime_expires
 ON photo.v30_import_adopter_runtime_leases(expires_at);

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_import_adopter_runtime(p_instance uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid THEN
  RETURN false;
 END IF;
 UPDATE photo.v30_import_adopter_runtime_leases
 SET expires_at=clock_timestamp()+interval '30 seconds'
 WHERE instance_id=p_instance;
 IF FOUND THEN RETURN true;END IF;
 UPDATE photo.v30_import_adopter_runtime_leases
 SET instance_id=p_instance,expires_at=clock_timestamp()+interval '30 seconds'
 WHERE instance_id=(SELECT old.instance_id FROM photo.v30_import_adopter_runtime_leases old
  WHERE old.expires_at<clock_timestamp()-interval '1 day'
  ORDER BY old.expires_at LIMIT 1 FOR UPDATE SKIP LOCKED);
 IF FOUND THEN RETURN true;END IF;
 INSERT INTO photo.v30_import_adopter_runtime_leases(instance_id,expires_at)
 VALUES(p_instance,clock_timestamp()+interval '30 seconds')
 ON CONFLICT(instance_id) DO UPDATE SET expires_at=excluded.expires_at;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_import_adopter_runtime(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_import_adopter_runtime(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_import_adopter_runtime(uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.withdraw_v30_import_adopter_runtime(p_instance uuid)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 UPDATE photo.v30_import_adopter_runtime_leases
 SET expires_at=clock_timestamp()-interval '1 second'
 WHERE instance_id=p_instance;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.withdraw_v30_import_adopter_runtime(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdraw_v30_import_adopter_runtime(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.withdraw_v30_import_adopter_runtime(uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_import_runtime_ready()
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.v30_import_runtime_leases
  WHERE expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.v30_import_adopter_runtime_leases
  WHERE expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_runtime_ready() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_runtime_ready() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_runtime_ready() TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
