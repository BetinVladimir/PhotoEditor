-- +goose Up
-- Preserve generic sealing; render recipes require exact semantic root/source pins.
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) RENAME TO seal_v2_task_inputs_pre_v26;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs_pre_v26(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v2_task_inputs(p_tenant uuid,p_run uuid,p_task uuid,p_manifest jsonb,p_chunks jsonb,
 p_entry_count integer,p_entries_sha text,p_direct_roots jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; node jsonb; binding jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF t.operation='render.compose' OR r.config_snapshot ? 'v26_render' THEN
  IF NOT photo.v2_input_document_scope(p_tenant,p_run,p_task,'v2_input_manifest')
   OR p_entry_count IS NULL OR p_entry_count NOT BETWEEN 2 AND 4096
   OR jsonb_typeof(p_direct_roots) IS DISTINCT FROM 'array'
   OR jsonb_array_length(p_direct_roots)<>1 THEN
   RAISE EXCEPTION 'V26_RENDER_INPUT_SCOPE_INVALID' USING ERRCODE='23514';END IF;
  SELECT value INTO node FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes')
   WHERE value->>'task_id'=p_task::text;
  binding:=(node->>'params_jcs')::jsonb->'scalars'->'source';
  IF p_direct_roots->0 IS DISTINCT FROM node->'params'
   OR (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='FrozenRenderRecipe')<>1
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
    AND i.contract='FrozenRenderRecipe' AND i.artifact_id=t.params_artifact_id AND i.sha256=node->'params'->>'sha256')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
    AND i.contract='WorkingImage' AND i.artifact_id::text=binding->'source'->>'artifact_id'
    AND i.sha256=binding->'source'->>'sha256') THEN
   RAISE EXCEPTION 'V26_RENDER_INPUT_ROOT_INVALID' USING ERRCODE='23514';END IF;
 END IF;
 RETURN photo.seal_v2_task_inputs_pre_v26(p_tenant,p_run,p_task,p_manifest,p_chunks,p_entry_count,p_entries_sha,p_direct_roots);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
