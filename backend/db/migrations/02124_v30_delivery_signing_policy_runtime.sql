-- +goose Up
-- Tenant installation alone cannot distinguish two deployed policy versions.
ALTER TABLE photo.v30_delivery_runtime_leases ADD COLUMN signing_policy_pins jsonb NOT NULL DEFAULT '[]'::jsonb
 CHECK(jsonb_typeof(signing_policy_pins)='array' AND jsonb_array_length(signing_policy_pins)<=4096);

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_delivery_runtime_pinned(p_instance uuid,p_bucket text,p_signing_tenants uuid[],p_pins jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR p_signing_tenants IS NULL OR cardinality(p_signing_tenants)>4096
 OR EXISTS(SELECT 1 FROM unnest(p_signing_tenants) tenant(id)
   WHERE id IS NULL OR id='00000000-0000-0000-0000-000000000000'::uuid)
 OR cardinality(p_signing_tenants) IS DISTINCT FROM
   (SELECT count(DISTINCT id) FROM unnest(p_signing_tenants) tenant(id))
 OR p_pins IS NULL OR jsonb_typeof(p_pins) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_pins)>4096 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_pins) pin(value)
  WHERE jsonb_typeof(value) IS DISTINCT FROM 'object'
   OR jsonb_typeof(value->'tenant_id') IS DISTINCT FROM 'string'
   OR jsonb_typeof(value->'policy_id') IS DISTINCT FROM 'string'
   OR jsonb_typeof(value->'policy_sha') IS DISTINCT FROM 'string'
   OR coalesce(value->>'policy_sha','') !~ '^[0-9a-f]{64}$'
   OR (value->>'tenant_id')::uuid IS NULL
   OR (value->>'policy_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
   OR NOT (value->>'tenant_id')::uuid=ANY(p_signing_tenants)) THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(p_pins)) IS DISTINCT FROM
    (SELECT count(DISTINCT (value->>'tenant_id',value->>'policy_id'))
     FROM jsonb_array_elements(p_pins) pin(value)) THEN RETURN false;END IF;
 INSERT INTO photo.v30_delivery_runtime_leases(instance_id,bucket,signing_tenants,signing_policy_pins,expires_at)
 VALUES(p_instance,p_bucket,p_signing_tenants,p_pins,clock_timestamp()+interval '30 seconds')
 ON CONFLICT(instance_id) DO UPDATE SET bucket=excluded.bucket,
  signing_tenants=excluded.signing_tenants,signing_policy_pins=excluded.signing_policy_pins,
  expires_at=excluded.expires_at;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb) TO photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime_bound(uuid,text,uuid[]) FROM photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signing_runtime_ready(p_bucket text,p_tenant uuid,p_policy uuid,p_sha text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT length(p_bucket) BETWEEN 3 AND 63 AND p_tenant IS NOT NULL
 AND p_policy IS NOT NULL AND p_sha ~ '^[0-9a-f]{64}$' AND EXISTS(
  SELECT 1 FROM photo.v30_delivery_runtime_leases l
  WHERE l.bucket=p_bucket AND l.expires_at>clock_timestamp()
   AND p_tenant=ANY(l.signing_tenants)
   AND l.signing_policy_pins @> jsonb_build_array(jsonb_build_object(
    'tenant_id',p_tenant,'policy_id',p_policy,'policy_sha',p_sha)));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid,uuid,text) TO photo_api;
REVOKE EXECUTE ON FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid) FROM photo_api;

-- +goose StatementBegin
DO $$
DECLARE definition text;old_call text;new_call text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text)'::regprocedure);
 old_call:='photo.v30_delivery_signing_runtime_ready(p_bucket,p_tenant)';
 new_call:='photo.v30_delivery_signing_runtime_ready(p_bucket,p_tenant,'
  ||'((policy->>''body_jcs'')::jsonb->>''signing_policy_id'')::uuid,'
  ||'(policy->>''body_jcs'')::jsonb->>''signing_policy_sha'')';
 IF (length(definition)-length(replace(definition,old_call,'')))/length(old_call)<>1 THEN
  RAISE EXCEPTION 'Unexpected delivery begin signing gate';END IF;
 EXECUTE replace(definition,old_call,new_call);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE definition text;old_call text;new_call text;
BEGIN
 definition:=pg_get_functiondef('photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text)'::regprocedure);
 old_call:='photo.v30_delivery_signing_runtime_ready(p_bucket,p_tenant)';
 new_call:='photo.v30_delivery_signing_runtime_ready(p_bucket,p_tenant,'
  ||'(SELECT ((value->>''body_jcs'')::jsonb->>''signing_policy_id'')::uuid'
  ||' FROM jsonb_array_elements(ledger.response_body->''documents'')'
  ||' WHERE value->>''purpose''=''v30_delivery_policy''),'
  ||'(SELECT (value->>''body_jcs'')::jsonb->>''signing_policy_sha'''
  ||' FROM jsonb_array_elements(ledger.response_body->''documents'')'
  ||' WHERE value->>''purpose''=''v30_delivery_policy''))';
 IF (length(definition)-length(replace(definition,old_call,'')))/length(old_call)<>1 THEN
  RAISE EXCEPTION 'Unexpected delivery activation signing gate';END IF;
 EXECUTE replace(definition,old_call,new_call);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
