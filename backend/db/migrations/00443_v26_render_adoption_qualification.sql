-- +goose Up
-- Repeat live trusted qualification in the final DB-only adoption transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_adoption_inputs_ready(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 node jsonb; params jsonb; matches integer; recipe_jcs text; recipe_sha text;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=t.run_id;
 IF t.id IS NULL OR r.id IS NULL OR w.id IS NULL OR t.operation<>'render.compose'
  OR t.operation_version<>'1' OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
  OR r.status NOT IN('queued','running') OR w.renderer_version<>'v2.2'
  OR NOT photo.v2_input_closure_ready(p_task)
  OR jsonb_typeof(r.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT count(*) INTO matches FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') n
  WHERE n->>'task_id'=p_task::text;
 IF matches<>1 THEN RETURN false;END IF;
 SELECT n INTO node FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') n
  WHERE n->>'task_id'=p_task::text;
 recipe_jcs:=node->>'params_jcs'; recipe_sha:=node->'params'->>'sha256';
 IF recipe_jcs IS NULL OR octet_length(recipe_jcs)>1048576
  OR encode(sha256(convert_to(recipe_jcs,'UTF8')),'hex') IS DISTINCT FROM recipe_sha
  OR node->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',t.params_artifact_id::text,'sha256',recipe_sha)
  OR node->>'input_digest' IS DISTINCT FROM t.input_digest
  OR node->'profile' IS DISTINCT FROM
   (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') THEN RETURN false;END IF;
 params:=recipe_jcs::jsonb;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.v26_render_direct_inputs_ready(p_tenant,p_task,w.project_id,r.created_by,
  (r.config_snapshot->'v26_render'->>'auth_session_id')::uuid,params) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.artifact_id=t.params_artifact_id AND i.sha256=recipe_sha AND i.contract='FrozenRenderRecipe') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IN('','null')
    OR a.object_version IS NULL OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.v26_render_recipe_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
  t.params_artifact_id,recipe_sha,r.created_by,(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid)
 OR NOT photo.v2_draft_artifact_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
  (params->'recipe'->'base_style'->>'artifact_id')::uuid,params->'recipe'->'base_style'->>'sha256','StyleV2',r.created_by,
  (r.config_snapshot->'v26_render'->>'auth_session_id')::uuid) THEN RETURN false;END IF;
 IF params->'learning' IS NOT NULL AND params->'learning'<>'null'::jsonb THEN
  IF NOT photo.v26_render_model_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
   t.params_artifact_id,recipe_sha,(params->'learning'->'model'->>'artifact_id')::uuid,params->'learning'->'model'->>'sha256',
   (params->'learning'->>'version_id')::uuid,r.created_by,(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid)
  OR NOT photo.v26_render_descriptor_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
   t.params_artifact_id,recipe_sha,(params->'learning'->'descriptor'->>'artifact_id')::uuid,params->'learning'->'descriptor'->>'sha256',
   (params->'learning'->>'analysis_id')::uuid,(params->'learning'->>'ordinal')::integer,
   params->'scalars'->>'model_lock_sha',r.created_by,(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid) THEN RETURN false;END IF;
 END IF;
 IF params->'recipe'->'layer_stack' IS NOT NULL AND params->'recipe'->'layer_stack'<>'null'::jsonb THEN
  IF NOT photo.v26_render_layer_stack_scope(p_tenant,w.project_id,r.created_by,
   (r.config_snapshot->'v26_render'->>'auth_session_id')::uuid,params,node,p_task)
   OR jsonb_array_length((node->>'layer_jcs')::jsonb->'layers')<>0 THEN RETURN false;END IF;
 END IF;
 -- User protection must retain its immutable published root/binary binding.
 IF NOT photo.v26_render_protection_scope(p_tenant,w.project_id,r.created_by,
  (r.config_snapshot->'v26_render'->>'auth_session_id')::uuid,params,node,p_task) THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v26_render_adoption_inputs_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_adoption_inputs_ready(uuid,uuid) FROM PUBLIC;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_render_qualification;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_render_qualification(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 IF t.operation='render.compose' THEN
  SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
  IF r.id IS NULL OR t.operation_version<>'1' OR t.attempt IS DISTINCT FROM p_attempt
   OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result THEN RETURN false;END IF;
  -- Already accepted immutable evidence retains the core's exact digest/replay
  -- conflict checks. A replay never creates new links or requalifies GPU work.
  IF NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(p_task,p_result)) THEN
   IF t.state<>'succeeded' OR t.cancel_requested OR NOT photo.v26_render_live_qualification_ready(p_tenant,r.id)
    OR NOT photo.v26_render_adoption_inputs_ready(p_tenant,p_task) THEN RETURN false;END IF;
  END IF;
 END IF;
 RETURN photo.adopt_v2_plan_pre_render_qualification(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
