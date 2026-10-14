-- +goose Up
-- Preserve the full admission proof while separating the live adopter check.
-- The diagnostic never treats a stale or unauthorized source as unavailable.
-- +goose StatementBegin
DO $$
DECLARE definition text; runtime_gate constant text :=
 ' IF photo.v31_signing_adopter_runtime_ready(p_bucket,p_params) IS DISTINCT FROM true THEN RETURN false;END IF;';
BEGIN
 definition := pg_get_functiondef('photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,runtime_gate,'')))/length(runtime_gate)<>1
 OR position('FUNCTION photo.v31_signing_admission_ready(' IN definition)=0 THEN
  RAISE EXCEPTION 'Unexpected V31 admission definition';END IF;
 definition := replace(definition,'FUNCTION photo.v31_signing_admission_ready(',
  'FUNCTION photo.v31_signing_admission_without_runtime(');
 definition := replace(definition,runtime_gate,'');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_signing_admission_without_runtime(uuid,uuid,uuid,uuid,jsonb,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_admission_without_runtime(uuid,uuid,uuid,uuid,jsonb,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_adopter_unavailable(
 p_tenant uuid,p_actor uuid,p_session uuid,p_body text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb; params text;bucket text;export_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_body IS NULL
 OR octet_length(p_body) NOT BETWEEN 2 AND 10485760 THEN RETURN false;END IF;
 BEGIN saved:=p_body::jsonb;export_id:=(saved->>'export_id')::uuid;
 EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
 IF saved->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR saved->>'actor_id' IS DISTINCT FROM p_actor::text
 OR jsonb_typeof(saved->'source_pins') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 params:=saved->>'params_jcs';bucket:=saved->>'bucket';
 IF photo.v31_signing_admission_without_runtime(p_tenant,p_actor,p_session,export_id,
   saved->'source_pins',bucket,params) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.v31_signing_adopter_runtime_ready(bucket,params) IS DISTINCT FROM true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_signing_adopter_unavailable(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_adopter_unavailable(uuid,uuid,uuid,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_signing_adopter_unavailable(uuid,uuid,uuid,text) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
