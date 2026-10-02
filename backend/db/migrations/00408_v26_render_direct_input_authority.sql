-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_direct_inputs_ready(p_tenant uuid,p_task uuid,p_project uuid,p_actor uuid,p_session uuid,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ref jsonb; kind text; version_id uuid; pairs jsonb;
BEGIN
 version_id:=(p_body->'scalars'->'source'->>'asset_version_id')::uuid;
 pairs:=jsonb_build_array(jsonb_build_object('ref',p_body->'recipe'->'base_style','contract','StyleV2'));
 IF p_body->'recipe'->'session_corrections' IS NOT NULL AND p_body->'recipe'->'session_corrections'<>'null'::jsonb THEN
  pairs:=pairs||jsonb_build_array(jsonb_build_object('ref',p_body->'recipe'->'session_corrections','contract','CorrectionPlan'));END IF;
 IF p_body->'recipe' ? 'overrides' THEN
  IF jsonb_typeof(p_body->'recipe'->'overrides') IS DISTINCT FROM 'array'
   OR jsonb_array_length(p_body->'recipe'->'overrides')>32 THEN RETURN false;END IF;
  FOR ref IN SELECT value FROM jsonb_array_elements(p_body->'recipe'->'overrides') LOOP
   pairs:=pairs||jsonb_build_array(jsonb_build_object('ref',ref,'contract','Override'));END LOOP;
 END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(pairs) LOOP
  kind:=ref->>'contract';ref:=ref->'ref';
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.contract=kind AND i.artifact_id::text=ref->>'artifact_id' AND i.sha256=ref->>'sha256')
  OR NOT photo.v2_draft_artifact_scope(p_tenant,p_project,version_id,(ref->>'artifact_id')::uuid,ref->>'sha256',kind,p_actor,p_session) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_direct_inputs_ready(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_direct_inputs_ready(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
-- Attempt-fenced admission for full render roots and learning pins.
-- Authored layer/protection binding remains an explicit outstanding gate.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.admit_v26_render_recipe(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 slot photo.resource_slots%ROWTYPE; node jsonb; params jsonb; matches integer;
BEGIN
 IF p_json IS NULL OR octet_length(p_json)>1048576 OR p_attempt IS NULL OR p_attempt<1
  OR p_fence IS NULL OR p_fence<1 OR p_sha IS NULL
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task;
 IF t.id IS NULL OR t.operation<>'render.compose' OR t.operation_version<>'1' OR t.resource_class<>'gpu'
  OR t.state<>'running' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
  OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
  OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
  OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
  OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
  OR t.execution_profile->>'backend' IS NULL
  OR t.execution_profile->>'backend' NOT IN('cpu','cuda','mps')
  OR NOT EXISTS(SELECT 1 FROM photo.worker_registrations reg WHERE reg.tenant_id=p_tenant
   AND reg.worker_id=p_worker AND reg.db_principal=session_user AND reg.enabled)
  OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task;
 IF slot.id IS NULL OR slot.fencing_token IS DISTINCT FROM p_fence
  OR slot.resource_class IS DISTINCT FROM 'gpu'
  OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()
  OR NOT photo.worker_authorized(p_tenant,p_worker,'gpu',slot.node_id,t.operation,
   t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
  OR r.status NOT IN('queued','running') OR w.renderer_version<>'v2.2'
  OR NOT photo.v2_input_closure_ready(p_task) THEN RETURN false;END IF;
 SELECT count(*) INTO matches FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') n
  WHERE n->>'task_id'=p_task::text;
 IF matches<>1 THEN RETURN false;END IF;
 SELECT n INTO node FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') n
  WHERE n->>'task_id'=p_task::text;
 IF node->>'params_jcs' IS DISTINCT FROM p_json
  OR node->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params::text,'sha256',p_sha)
  OR node->>'input_digest' IS DISTINCT FROM p_digest
  OR node->'profile' IS DISTINCT FROM
   (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') THEN RETURN false;END IF;
 params:=p_json::jsonb;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.v26_render_direct_inputs_ready(p_tenant,p_task,w.project_id,r.created_by,
  (r.config_snapshot->'v26_render'->>'auth_session_id')::uuid,params) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.artifact_id=p_params AND i.sha256=p_sha AND i.contract='FrozenRenderRecipe') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IN('','null')
    OR a.object_version IS NULL OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.v26_render_recipe_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
  p_params,p_sha,r.created_by,(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid)
 OR NOT photo.v2_draft_artifact_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
  (params->'recipe'->'base_style'->>'artifact_id')::uuid,params->'recipe'->'base_style'->>'sha256','StyleV2',r.created_by,
  (r.config_snapshot->'v26_render'->>'auth_session_id')::uuid) THEN RETURN false;END IF;
 IF params->'learning' IS NOT NULL AND params->'learning'<>'null'::jsonb THEN
  IF NOT photo.v26_render_model_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
   p_params,p_sha,(params->'learning'->'model'->>'artifact_id')::uuid,params->'learning'->'model'->>'sha256',
   (params->'learning'->>'version_id')::uuid,r.created_by,(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid)
  OR NOT photo.v26_render_descriptor_scope(p_tenant,w.project_id,(params->'scalars'->'source'->>'asset_version_id')::uuid,
   p_params,p_sha,(params->'learning'->'descriptor'->>'artifact_id')::uuid,params->'learning'->'descriptor'->>'sha256',
   (params->'learning'->>'analysis_id')::uuid,(params->'learning'->>'ordinal')::integer,
   params->'scalars'->>'model_lock_sha',r.created_by,(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid) THEN RETURN false;END IF;
 END IF;
 -- Layer/protection authority requires its dedicated full-render SQL binding;
 -- keep these paths closed until that proof is implemented.
 IF params->'recipe'->'layer_stack' IS NOT NULL AND params->'recipe'->'layer_stack'<>'null'::jsonb
 OR params->'recipe'->'protected_mask' IS NOT NULL AND params->'recipe'->'protected_mask'<>'null'::jsonb THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
