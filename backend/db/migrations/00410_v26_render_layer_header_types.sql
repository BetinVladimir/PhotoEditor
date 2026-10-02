-- +goose Up
-- Match typed LayerStack root header fields at SQL staging.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_layer_stack_scope(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_body jsonb,p_node jsonb,p_task uuid)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ref jsonb; layer jsonb; raw text; binding jsonb;
BEGIN
 ref:=p_body->'recipe'->'layer_stack';raw:=p_node->>'layer_jcs';binding:=p_body->'scalars'->'source';
 IF ref IS NULL OR ref='null'::jsonb THEN RETURN raw IS NULL OR raw='';END IF;
 IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb
  OR jsonb_typeof(binding) IS DISTINCT FROM 'object'
  OR binding-'asset_version_id'-'source'-'canvas_hash'<>'{}'::jsonb THEN RETURN false;END IF;
 IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM ref->>'sha256' THEN RETURN false;END IF;
 layer:=raw::jsonb;
 IF jsonb_typeof(layer) IS DISTINCT FROM 'object' OR layer-'schema_version'-'source'-'layers'<>'{}'::jsonb
  OR jsonb_typeof(layer->'schema_version') IS DISTINCT FROM 'number'
  OR layer->>'schema_version' IS DISTINCT FROM '1' OR layer->'source' IS DISTINCT FROM binding
  OR jsonb_typeof(layer->'layers') IS DISTINCT FROM 'array' OR jsonb_array_length(layer->'layers')>32 THEN RETURN false;END IF;
 IF p_task IS NOT NULL AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.contract='LayerStack' AND i.artifact_id::text=ref->>'artifact_id' AND i.sha256=ref->>'sha256') THEN RETURN false;END IF;
 RETURN photo.v2_draft_artifact_scope(p_tenant,p_project,(binding->>'asset_version_id')::uuid,
  (ref->>'artifact_id')::uuid,ref->>'sha256','LayerStack',p_actor,p_session);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
