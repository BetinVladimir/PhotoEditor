-- +goose Up
-- A signer worker is not proof that an independent C2PA result adopter runs.
-- The lease is global process state, scoped to the object bucket and exact
-- installed policy ID/SHA. No tenant metadata is stored in this table.
CREATE TABLE photo.v31_signing_adopter_runtime_leases (
 instance_id uuid PRIMARY KEY,
 bucket text NOT NULL CHECK (length(bucket) BETWEEN 3 AND 63),
 pins jsonb NOT NULL CHECK (jsonb_typeof(pins)='array'),
 expires_at timestamptz NOT NULL CHECK (isfinite(expires_at))
);
ALTER TABLE photo.v31_signing_adopter_runtime_leases OWNER TO photo_fn_owner;
ALTER TABLE photo.v31_signing_adopter_runtime_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v31_signing_adopter_runtime_leases FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v31_signing_adopter_runtime_leases
 TO photo_fn_owner USING (true) WITH CHECK (true);
REVOKE ALL ON photo.v31_signing_adopter_runtime_leases FROM PUBLIC,photo_fn_owner,
 photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT SELECT,INSERT,UPDATE ON photo.v31_signing_adopter_runtime_leases TO photo_fn_owner;
CREATE INDEX v31_signing_adopter_runtime_expires
 ON photo.v31_signing_adopter_runtime_leases(expires_at);

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v31_signing_adopter_runtime(p_instance uuid,p_bucket text,p_pins jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb;seen text[]:='{}';identity text;total integer;
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR jsonb_typeof(p_pins) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 total:=jsonb_array_length(p_pins);
 IF total NOT BETWEEN 1 AND 256 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_pins) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR NOT (item ? 'id' AND item ? 'sha') OR
   (SELECT count(*) FROM jsonb_object_keys(item))<>2 OR
   item->>'id' !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
   item->>'id'='00000000-0000-0000-0000-000000000000' OR
   item->>'sha' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  identity:=item->>'id'||':'||item->>'sha';
  IF identity=ANY(seen) THEN RETURN false;END IF;
  seen:=array_append(seen,identity);
 END LOOP;
 UPDATE photo.v31_signing_adopter_runtime_leases
 SET bucket=p_bucket,pins=p_pins,expires_at=clock_timestamp()+interval '30 seconds'
 WHERE instance_id=p_instance;
 IF FOUND THEN RETURN true;END IF;
 UPDATE photo.v31_signing_adopter_runtime_leases
 SET instance_id=p_instance,bucket=p_bucket,pins=p_pins,
  expires_at=clock_timestamp()+interval '30 seconds'
 WHERE instance_id=(SELECT old.instance_id FROM photo.v31_signing_adopter_runtime_leases old
  WHERE old.expires_at<clock_timestamp()-interval '1 day'
  ORDER BY old.expires_at LIMIT 1 FOR UPDATE SKIP LOCKED);
 IF FOUND THEN RETURN true;END IF;
 INSERT INTO photo.v31_signing_adopter_runtime_leases(instance_id,bucket,pins,expires_at)
 VALUES(p_instance,p_bucket,p_pins,clock_timestamp()+interval '30 seconds')
 ON CONFLICT(instance_id) DO UPDATE SET bucket=excluded.bucket,pins=excluded.pins,
  expires_at=excluded.expires_at;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v31_signing_adopter_runtime(uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v31_signing_adopter_runtime(uuid,text,jsonb) FROM PUBLIC,
 photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.announce_v31_signing_adopter_runtime(uuid,text,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.withdraw_v31_signing_adopter_runtime(p_instance uuid)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 UPDATE photo.v31_signing_adopter_runtime_leases
 SET expires_at=clock_timestamp()-interval '1 second' WHERE instance_id=p_instance;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.withdraw_v31_signing_adopter_runtime(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdraw_v31_signing_adopter_runtime(uuid) FROM PUBLIC,
 photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.withdraw_v31_signing_adopter_runtime(uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_adopter_runtime_ready(p_bucket text,p_params text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE params jsonb;policy text;sha text;
BEGIN
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR
  p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 BEGIN params:=p_params::jsonb;
 EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 policy:=params->>'signing_policy_id';sha:=params->'policy'->>'sha256';
 IF policy IS NULL OR policy !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR policy='00000000-0000-0000-0000-000000000000' OR
 sha IS NULL OR sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v31_signing_adopter_runtime_leases runtime
  WHERE runtime.bucket=p_bucket AND runtime.expires_at>clock_timestamp()
   AND runtime.pins @> jsonb_build_array(jsonb_build_object('id',policy,'sha',sha)));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_signing_adopter_runtime_ready(text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_adopter_runtime_ready(text,text) FROM PUBLIC,
 photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_signing_adopter_runtime_ready(text,text) TO photo_api;

-- Pending admission checks the same process that can physically adopt the
-- exact immutable policy. Existing completed ticket replay stays earlier.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle constant text:=' rid:=photo.v31_signing_lineage_scope(';
BEGIN
 definition:=pg_get_functiondef('photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected V31 signing admission definition';END IF;
 EXECUTE replace(definition,needle,
  ' IF photo.v31_signing_adopter_runtime_ready(p_bucket,p_params) IS DISTINCT FROM true THEN RETURN false;END IF;'
  ||needle);
END $$;
-- +goose StatementEnd

-- The current activation wrapper calls this predecessor. Insert after its
-- completed-ticket replay branch and before any new graph seal/dispatch.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle constant text:=
 ' IF photo.v31_signing_transport_ready(p_tenant,p_actor,p_session,p_key,p_request_sha,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;';
BEGIN
 definition:=pg_get_functiondef('photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected V31 signing activation definition';END IF;
 EXECUTE replace(definition,needle,
  ' IF photo.v31_signing_adopter_runtime_ready(saved->>''bucket'',saved->>''params_jcs'') IS DISTINCT FROM true THEN RETURN false;END IF;'
  ||needle);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
