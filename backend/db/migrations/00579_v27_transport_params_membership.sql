-- +goose Up
-- Exact Params membership is mandatory before accepting a transport batch.
-- This remains a private component, not a child admission entrypoint.
-- +goose StatementBegin
ALTER FUNCTION photo.semantic_approved_transport_batch_ready(uuid,uuid,jsonb,jsonb) RENAME TO semantic_approved_transport_batch_pre_params;
REVOKE ALL ON FUNCTION photo.semantic_approved_transport_batch_pre_params(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_scheduler,photo_worker;
CREATE FUNCTION photo.semantic_approved_transport_batch_ready(p_tenant uuid,p_parent uuid,p_nodes jsonb,p_transport jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb; transport jsonb;
BEGIN
 IF jsonb_typeof(p_nodes) IS DISTINCT FROM 'array' OR jsonb_typeof(p_transport) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_nodes) NOT BETWEEN 1 AND 100 OR jsonb_array_length(p_transport)<>jsonb_array_length(p_nodes) THEN RETURN false;END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(p_nodes) LOOP
  IF jsonb_typeof(node) IS DISTINCT FROM 'object' OR jsonb_typeof(node->'id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(node->'params_artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(node->'params_sha') IS DISTINCT FROM 'string' OR coalesce(node->>'params_sha','') !~ '^[0-9a-f]{64}$'
  OR (node->>'params_artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
  SELECT value INTO transport FROM jsonb_array_elements(p_transport) WHERE value->>'task_id'=node->>'id';
  IF transport IS NULL OR jsonb_typeof(transport->'direct_roots') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(transport->'direct_roots') r
   WHERE r=jsonb_build_object('artifact_id',node->>'params_artifact_id','sha256',node->>'params_sha')) THEN RETURN false;END IF;
 END LOOP;
 RETURN photo.semantic_approved_transport_batch_pre_params(p_tenant,p_parent,p_nodes,p_transport);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_transport_batch_ready(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_transport_batch_ready(uuid,uuid,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
