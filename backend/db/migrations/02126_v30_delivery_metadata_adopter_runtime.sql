-- +goose Up
-- Metadata preparation is not proof that its independent adoption loop runs.
ALTER TABLE photo.v30_delivery_runtime_leases ADD COLUMN metadata_adopter_ready boolean NOT NULL DEFAULT false;

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_delivery_runtime_complete(p_instance uuid,p_bucket text,p_signing_tenants uuid[],p_pins jsonb,p_metadata_ready boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_metadata_ready IS NULL OR photo.announce_v30_delivery_runtime_windowed(
  p_instance,p_bucket,p_signing_tenants,p_pins) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.v30_delivery_runtime_leases SET metadata_adopter_ready=p_metadata_ready
 WHERE instance_id=p_instance AND bucket=p_bucket;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_delivery_runtime_complete(uuid,text,uuid[],jsonb,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_delivery_runtime_complete(uuid,text,uuid[],jsonb,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime_complete(uuid,text,uuid[],jsonb,boolean) TO photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime_windowed(uuid,text,uuid[],jsonb) FROM photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_metadata_runtime_ready(p_bucket text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT length(p_bucket) BETWEEN 3 AND 63 AND EXISTS(
  SELECT 1 FROM photo.v30_delivery_runtime_leases
  WHERE bucket=p_bucket AND metadata_adopter_ready
   AND expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_metadata_runtime_ready(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_metadata_runtime_ready(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_metadata_runtime_ready(text) TO photo_api;

-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text)'::regprocedure);
 needle:=' IF photo.v30_delivery_runtime_ready(p_bucket) IS DISTINCT FROM true THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected delivery begin runtime gate';END IF;
 EXECUTE replace(definition,needle,needle||
  ' IF (policy->>''body_jcs'')::jsonb->>''metadata_policy'' IS DISTINCT FROM ''strip'''
  ||' AND photo.v30_delivery_metadata_runtime_ready(p_bucket) IS DISTINCT FROM true'
  ||' THEN RETURN false;END IF;');
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text)'::regprocedure);
 needle:=' IF photo.v30_delivery_runtime_ready(p_bucket) IS DISTINCT FROM true THEN';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected delivery activation runtime gate';END IF;
 EXECUTE replace(definition,needle,$gate$
 IF (SELECT count(*)=1 AND min((value->>'body_jcs')::jsonb->>'metadata_policy') IN
   ('strip','copyright','approved_exif')
   FROM jsonb_array_elements(ledger.response_body->'documents')
   WHERE value->>'purpose'='v30_delivery_policy') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(ledger.response_body->'documents')
   WHERE value->>'purpose'='v30_delivery_policy'
    AND (value->>'body_jcs')::jsonb->>'metadata_policy' IN ('copyright','approved_exif'))
   AND photo.v30_delivery_metadata_runtime_ready(p_bucket) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'DELIVERY_METADATA_RUNTIME_UNAVAILABLE' USING ERRCODE='P0001';
 END IF;
$gate$||needle);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
