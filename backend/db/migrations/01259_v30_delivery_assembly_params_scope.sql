-- +goose Up
-- Params publication is bound to a complete retained current assembly root.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_assembly_params_scope(p_expansion uuid,p_token uuid,p_params text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE params jsonb;ref jsonb;e photo.graph_expansions%ROWTYPE;raw text;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 params:=p_params::jsonb;ref:=params->'assembly';
 IF params-'schema_version'-'assembly'<>'{}'::jsonb OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb
 OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 raw:=photo.v30_delivery_saved_assembly(p_expansion,p_token,(ref->>'artifact_id')::uuid,ref->>'sha256');
 IF raw IS NULL OR photo.retain_v30_delivery_assembly(p_expansion,p_token,(ref->>'artifact_id')::uuid,ref->>'sha256') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 RETURN jsonb_build_object('tenant',e.tenant_id,'run',e.run_id,
 'owner',photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),'v2.delivery.assemble:0'),
 'sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'),'bytes',octet_length(p_params));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_assembly_params_scope(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_assembly_params_scope(uuid,uuid,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
