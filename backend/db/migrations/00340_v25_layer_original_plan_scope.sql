-- +goose Up
-- Live authority for an original-source composition plan. Persistence must
-- repeat this predicate under the run/quota/snapshot locks.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_plan_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_workflow uuid,p_config jsonb,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb;
BEGIN
 IF NOT photo.v25_layer_plan_shape(p_workflow,p_config,p_bytes) THEN RETURN false;END IF;
 IF p_config->>'project_id' IS DISTINCT FROM p_project::text
  OR p_config->>'input_snapshot_id' IS DISTINCT FROM p_snapshot::text
  OR p_config->'v25_composition'->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN false;END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(p_config->'v25_composition'->'nodes') LOOP
  IF NOT photo.v25_layer_original_task_params_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,
   (node->>'task_id')::uuid,(node->'params'->>'artifact_id')::uuid,node->'params'->>'sha256',
   node->>'params_jcs',(node->>'width')::bigint,(node->>'height')::bigint,node->'profile') THEN RETURN false;END IF;
 END LOOP;
 -- Multiple layers may belong to one selected version, but every selected
 -- version must occur. Per-node source scope rejects versions outside snapshot.
 RETURN NOT EXISTS(SELECT 1 FROM photo.snapshot_items si WHERE si.tenant_id=p_tenant
  AND si.snapshot_id=p_snapshot AND NOT EXISTS(SELECT 1
   FROM jsonb_array_elements(p_config->'v25_composition'->'nodes') n
   WHERE ((n->>'params_jcs')::jsonb->'base'->>'asset_version_id')=si.asset_version_id::text));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_plan_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_plan_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_plan_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
