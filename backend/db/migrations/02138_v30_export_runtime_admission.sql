-- +goose Up
-- A new export graph is runnable only when its object bucket has a healthy
-- scheduler adoption loop. This is process state, not tenant data.
CREATE TABLE photo.v30_export_runtime_leases (
 instance_id uuid PRIMARY KEY,
 bucket text NOT NULL CHECK (length(bucket) BETWEEN 3 AND 63),
 expires_at timestamptz NOT NULL CHECK (isfinite(expires_at))
);
ALTER TABLE photo.v30_export_runtime_leases OWNER TO photo_fn_owner;
ALTER TABLE photo.v30_export_runtime_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_export_runtime_leases FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_export_runtime_leases
 TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.v30_export_runtime_leases FROM PUBLIC,photo_api,
 photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT SELECT,INSERT,UPDATE ON photo.v30_export_runtime_leases TO photo_fn_owner;
CREATE INDEX v30_export_runtime_expires ON photo.v30_export_runtime_leases(expires_at);

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_export_runtime(p_instance uuid,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 UPDATE photo.v30_export_runtime_leases
 SET expires_at=clock_timestamp()+interval '30 seconds',bucket=p_bucket
 WHERE instance_id=p_instance;
 IF FOUND THEN RETURN true;END IF;
 UPDATE photo.v30_export_runtime_leases
 SET instance_id=p_instance,expires_at=clock_timestamp()+interval '30 seconds',bucket=p_bucket
 WHERE instance_id=(SELECT old.instance_id FROM photo.v30_export_runtime_leases old
  WHERE old.expires_at<clock_timestamp()-interval '1 day'
  ORDER BY old.expires_at LIMIT 1 FOR UPDATE SKIP LOCKED);
 IF FOUND THEN RETURN true;END IF;
 INSERT INTO photo.v30_export_runtime_leases(instance_id,bucket,expires_at)
 VALUES(p_instance,p_bucket,clock_timestamp()+interval '30 seconds')
 ON CONFLICT(instance_id) DO UPDATE SET bucket=excluded.bucket,expires_at=excluded.expires_at;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_export_runtime(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_export_runtime(uuid,text) FROM PUBLIC,
 photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.announce_v30_export_runtime(uuid,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.withdraw_v30_export_runtime(p_instance uuid)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 UPDATE photo.v30_export_runtime_leases SET expires_at=clock_timestamp()-interval '1 second'
 WHERE instance_id=p_instance;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.withdraw_v30_export_runtime(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdraw_v30_export_runtime(uuid) FROM PUBLIC,
 photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.withdraw_v30_export_runtime(uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_runtime_ready(p_bucket text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT length(p_bucket) BETWEEN 3 AND 63 AND EXISTS(
  SELECT 1 FROM photo.v30_export_runtime_leases runtime
  WHERE runtime.bucket=p_bucket AND runtime.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_export_runtime_ready(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_runtime_ready(text) FROM PUBLIC,
 photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_export_runtime_ready(text) TO photo_api;

-- Preserve the existing draft authority and replay path. The only new gate is
-- on the draft-ready function, which activation calls after ticket replay.
-- +goose StatementBegin
DO $$
DECLARE definition text; old_gate constant text :=
 'RETURN EXISTS(SELECT 1 FROM photo.quota_reservations';
BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_export_draft_ready(uuid,uuid,uuid,text,text,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,old_gate,'')))/length(old_gate)<>1 THEN
  RAISE EXCEPTION 'Unexpected V30 export draft admission definition';
 END IF;
 EXECUTE replace(definition,old_gate,
  'RETURN photo.v30_export_runtime_ready((SELECT a.bucket FROM photo.artifacts a '
  ||'WHERE a.tenant_id=p_tenant AND a.id=params AND a.sha256=scope->>''sha'' '
  ||'AND a.state=''ready'')) IS TRUE AND EXISTS(SELECT 1 FROM photo.quota_reservations');
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
