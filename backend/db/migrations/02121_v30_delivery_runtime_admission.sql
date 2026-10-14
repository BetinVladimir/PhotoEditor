-- +goose Up
-- A scheduler advertises only a healthy delivery loop for its own object bucket.
CREATE TABLE photo.v30_delivery_runtime_leases (
 instance_id uuid PRIMARY KEY,
 bucket text NOT NULL CHECK(length(bucket) BETWEEN 3 AND 63),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at))
);
ALTER TABLE photo.v30_delivery_runtime_leases OWNER TO photo_fn_owner;
ALTER TABLE photo.v30_delivery_runtime_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_delivery_runtime_leases FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_delivery_runtime_leases TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v30_delivery_runtime_leases FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT SELECT,INSERT,UPDATE ON photo.v30_delivery_runtime_leases TO photo_fn_owner;
CREATE INDEX v30_delivery_runtime_expires ON photo.v30_delivery_runtime_leases(expires_at);

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_delivery_runtime(p_instance uuid,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
    OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false; END IF;
 INSERT INTO photo.v30_delivery_runtime_leases(instance_id,bucket,expires_at)
 VALUES(p_instance,p_bucket,clock_timestamp()+interval '30 seconds')
 ON CONFLICT(instance_id) DO UPDATE SET bucket=excluded.bucket,expires_at=excluded.expires_at;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_delivery_runtime(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_delivery_runtime(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime(uuid,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.withdraw_v30_delivery_runtime(p_instance uuid)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_delivery_runtime_leases SET expires_at=clock_timestamp() WHERE instance_id=p_instance;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.withdraw_v30_delivery_runtime(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdraw_v30_delivery_runtime(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.withdraw_v30_delivery_runtime(uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_runtime_ready(p_bucket text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT length(p_bucket) BETWEEN 3 AND 63 AND EXISTS(
  SELECT 1 FROM photo.v30_delivery_runtime_leases
  WHERE bucket=p_bucket AND expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_runtime_ready(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_runtime_ready(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_runtime_ready(text) TO photo_api;

-- +goose StatementBegin
DO $$
DECLARE definition text; old_signature text; old_guard text; old_name text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v30_delivery_create_run(uuid,uuid,uuid,text,text,text)'::regprocedure);
 old_name:='begin_v30_delivery_create_run(';
 old_signature:='p_config text)';
 old_guard:=' IF EXISTS(SELECT 1 FROM photo.runs WHERE id=rid) THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,old_name,'')))/length(old_name)<>1
  OR (length(definition)-length(replace(definition,old_signature,'')))/length(old_signature)<>1
  OR (length(definition)-length(replace(definition,old_guard,'')))/length(old_guard)<>1 THEN
  RAISE EXCEPTION 'Unexpected delivery begin definition';
 END IF;
 definition:=replace(definition,old_name,'begin_v30_delivery_create_run_bound(');
 definition:=replace(definition,old_signature,'p_config text, p_bucket text)');
 definition:=replace(definition,old_guard,
  ' IF photo.v30_delivery_runtime_ready(p_bucket) IS DISTINCT FROM true THEN RETURN false;END IF;'
  ||old_guard);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text) TO photo_api;
REVOKE EXECUTE ON FUNCTION photo.begin_v30_delivery_create_run(uuid,uuid,uuid,text,text,text) FROM photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
