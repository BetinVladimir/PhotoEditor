-- +goose Up
-- The exact installed policy may expire while a scheduler remains healthy.
-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_delivery_runtime_windowed(p_instance uuid,p_bucket text,p_signing_tenants uuid[],p_pins jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_pins IS NULL OR jsonb_typeof(p_pins) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_pins)>4096 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_pins) pin(value)
  WHERE jsonb_typeof(value) IS DISTINCT FROM 'object'
   OR jsonb_typeof(value->'not_before') IS DISTINCT FROM 'string'
   OR jsonb_typeof(value->'not_after') IS DISTINCT FROM 'string'
   OR NOT isfinite((value->>'not_before')::timestamptz)
   OR NOT isfinite((value->>'not_after')::timestamptz)
   OR (value->>'not_before')::timestamptz >= (value->>'not_after')::timestamptz)
 THEN RETURN false;END IF;
 RETURN photo.announce_v30_delivery_runtime_pinned(p_instance,p_bucket,p_signing_tenants,p_pins);
EXCEPTION WHEN invalid_datetime_format OR datetime_field_overflow OR invalid_text_representation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_delivery_runtime_windowed(uuid,text,uuid[],jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_delivery_runtime_windowed(uuid,text,uuid[],jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime_windowed(uuid,text,uuid[],jsonb) TO photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb) FROM photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_signing_runtime_ready(p_bucket text,p_tenant uuid,p_policy uuid,p_sha text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT length(p_bucket) BETWEEN 3 AND 63 AND p_tenant IS NOT NULL
 AND p_policy IS NOT NULL AND p_sha ~ '^[0-9a-f]{64}$' AND EXISTS(
  SELECT 1 FROM photo.v30_delivery_runtime_leases l,
   LATERAL jsonb_array_elements(l.signing_policy_pins) pin(value)
  WHERE l.bucket=p_bucket AND l.expires_at>clock_timestamp()
   AND p_tenant=ANY(l.signing_tenants)
   AND pin.value->>'tenant_id'=p_tenant::text
   AND pin.value->>'policy_id'=p_policy::text
   AND pin.value->>'policy_sha'=p_sha
   AND (pin.value->>'not_before')::timestamptz<=clock_timestamp()
   AND (pin.value->>'not_after')::timestamptz>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid,uuid,text) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
