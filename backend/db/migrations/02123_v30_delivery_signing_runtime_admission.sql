-- +goose Up
-- The ordinary scheduler lease does not prove that tenant signing is installed.
ALTER TABLE photo.v30_delivery_runtime_leases ADD COLUMN signing_tenants uuid[] NOT NULL DEFAULT '{}'::uuid[]
 CHECK(cardinality(signing_tenants)<=4096);

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_delivery_runtime_bound(p_instance uuid,p_bucket text,p_signing_tenants uuid[])
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
    OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
    OR p_signing_tenants IS NULL OR cardinality(p_signing_tenants)>4096
    OR EXISTS(SELECT 1 FROM unnest(p_signing_tenants) tenant(id)
      WHERE id IS NULL OR id='00000000-0000-0000-0000-000000000000'::uuid)
    OR cardinality(p_signing_tenants) IS DISTINCT FROM
       (SELECT count(DISTINCT id) FROM unnest(p_signing_tenants) tenant(id)) THEN RETURN false;
 END IF;
 INSERT INTO photo.v30_delivery_runtime_leases(instance_id,bucket,signing_tenants,expires_at)
 VALUES(p_instance,p_bucket,p_signing_tenants,clock_timestamp()+interval '30 seconds')
 ON CONFLICT(instance_id) DO UPDATE SET bucket=excluded.bucket,
  signing_tenants=excluded.signing_tenants,expires_at=excluded.expires_at;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_delivery_runtime_bound(uuid,text,uuid[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_delivery_runtime_bound(uuid,text,uuid[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime_bound(uuid,text,uuid[]) TO photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.announce_v30_delivery_runtime(uuid,text) FROM photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signing_runtime_ready(p_bucket text,p_tenant uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT length(p_bucket) BETWEEN 3 AND 63 AND p_tenant IS NOT NULL AND EXISTS(
  SELECT 1 FROM photo.v30_delivery_runtime_leases
  WHERE bucket=p_bucket AND p_tenant=ANY(signing_tenants) AND expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signing_runtime_ready(text,uuid) TO photo_api;

-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text)'::regprocedure);
 needle:=' IF photo.v30_delivery_runtime_ready(p_bucket) IS DISTINCT FROM true THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected delivery begin runtime gate';
 END IF;
 EXECUTE replace(definition,needle,needle||
  ' IF (policy->>''body_jcs'')::jsonb->>''provenance''=''required'' AND '
  ||'photo.v30_delivery_signing_runtime_ready(p_bucket,p_tenant) IS DISTINCT FROM true '
  ||'THEN RETURN false;END IF;');
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text)'::regprocedure);
 needle:=' IF photo.v30_delivery_runtime_ready(p_bucket) IS DISTINCT FROM true THEN';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected delivery activation runtime gate';
 END IF;
 EXECUTE replace(definition,needle,$gate$
 IF (SELECT count(*)=1 AND min((value->>'body_jcs')::jsonb->>'provenance') IN ('off','required')
     FROM jsonb_array_elements(ledger.response_body->'documents')
     WHERE value->>'purpose'='v30_delivery_policy') IS DISTINCT FROM true THEN RETURN NULL; END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(ledger.response_body->'documents')
   WHERE value->>'purpose'='v30_delivery_policy'
     AND (value->>'body_jcs')::jsonb->>'provenance'='required')
   AND photo.v30_delivery_signing_runtime_ready(p_bucket,p_tenant) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'DELIVERY_SIGNING_RUNTIME_UNAVAILABLE' USING ERRCODE='P0001';
 END IF;
$gate$||needle);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
