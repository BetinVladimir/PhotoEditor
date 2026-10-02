-- +goose Up
-- Stage full-recipe blocked tasks only; activation/admission remain separate.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.stage_v26_render_recipe_task(p_tenant uuid,p_run uuid,p_task uuid,p_ordinal integer,
 p_params uuid,p_sha text,p_digest text,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; old photo.tasks%ROWTYPE;
 node jsonb; body jsonb; binding jsonb; reserved bigint; key text; class text; session_id uuid; version_id uuid; resource_key text; task_hash bytea; expected_task uuid; source_body jsonb; width bigint; height bigint; pixels bigint; mapped bigint; tile_pixels bigint; planned_node jsonb; total_output numeric:=0;
BEGIN
 IF p_task IS NULL OR p_params IS NULL OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999
  OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_digest,'') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF p_profile->>'tile_size' NOT IN('256','512','1024') OR p_profile->>'tile_size' IS NULL
  OR jsonb_typeof(p_profile->'tile_size') IS DISTINCT FROM 'number'
  OR p_profile->>'alpha_mode' NOT IN('require_float32','normalize_uint16') OR p_profile->>'alpha_mode' IS NULL
  OR jsonb_typeof(p_profile->'resource_estimate') IS DISTINCT FROM 'object'
  OR NOT (p_profile->'resource_estimate') ?& ARRAY['pixel_count','ram_bytes','scratch_bytes','output_bytes','vram_bytes']
  OR ((p_profile->'resource_estimate')-'pixel_count'-'ram_bytes'-'scratch_bytes'-'output_bytes'-'vram_bytes')<>'{}'::jsonb THEN RETURN false;END IF;
 FOREACH resource_key IN ARRAY ARRAY['pixel_count','ram_bytes','scratch_bytes','output_bytes','vram_bytes'] LOOP
  IF jsonb_typeof(p_profile->'resource_estimate'->resource_key) IS DISTINCT FROM 'number'
   OR coalesce(p_profile->'resource_estimate'->>resource_key,'') !~ '^[0-9]+$'
   OR (p_profile->'resource_estimate'->>resource_key)::numeric>9007199254740991 THEN RETURN false;END IF;
 END LOOP;
 IF (p_profile->'resource_estimate'->>'pixel_count')::bigint NOT BETWEEN 1 AND 200000000
  OR (p_profile->'resource_estimate'->>'ram_bytes')::bigint<1
  OR (p_profile->'resource_estimate'->>'scratch_bytes')::bigint<1
  OR (p_profile->'resource_estimate'->>'output_bytes')::bigint<1 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.status<>'draft'
  OR r.graph_sealed OR r.cancel_requested OR w.workflow_type<>'preview' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 task_hash:=public.digest(uuid_send(w.id)||convert_to('render.compose:'||p_ordinal::text,'UTF8'),'sha1');
 task_hash:=set_byte(task_hash,6,(get_byte(task_hash,6)&15)|80);
 task_hash:=set_byte(task_hash,8,(get_byte(task_hash,8)&63)|128);
 expected_task:=encode(substring(task_hash FROM 1 FOR 16),'hex')::uuid;
 IF p_task IS DISTINCT FROM expected_task
  OR jsonb_typeof(r.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array'
  OR jsonb_array_length(r.config_snapshot->'v26_render'->'nodes') NOT BETWEEN 1 AND 5000
  OR p_ordinal>=jsonb_array_length(r.config_snapshot->'v26_render'->'nodes') THEN RETURN false;END IF;
 SELECT bytes INTO reserved FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run AND state='reserved'
  AND expires_at>clock_timestamp() FOR SHARE;
 IF reserved IS NULL THEN RETURN false;END IF;
 FOR planned_node IN SELECT value FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') LOOP
  IF jsonb_typeof(planned_node->'profile'->'resource_estimate'->'output_bytes') IS DISTINCT FROM 'number'
   OR coalesce(planned_node->'profile'->'resource_estimate'->>'output_bytes','') !~ '^[1-9][0-9]*$'
   OR (planned_node->'profile'->'resource_estimate'->>'output_bytes')::numeric>9007199254740991 THEN RETURN false;END IF;
  total_output:=total_output+(planned_node->'profile'->'resource_estimate'->>'output_bytes')::numeric;
  IF total_output>reserved THEN RETURN false;END IF;
 END LOOP;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id FOR SHARE;
 node:=r.config_snapshot->'v26_render'->'nodes'->p_ordinal;
 session_id:=(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid;
 IF node->>'task_id' IS DISTINCT FROM p_task::text
  OR node->'params'->>'artifact_id' IS DISTINCT FROM p_params::text
  OR node->'params'->>'sha256' IS DISTINCT FROM p_sha
  OR node->>'input_digest' IS DISTINCT FROM p_digest
  OR node->'profile' IS DISTINCT FROM p_profile
  OR node->>'params_jcs' IS NULL OR octet_length(node->>'params_jcs')>1048576
  OR encode(sha256(convert_to(node->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 body:=(node->>'params_jcs')::jsonb; binding:=body->'scalars'->'source';
 version_id:=(binding->>'asset_version_id')::uuid;
 IF node->>'source_jcs' IS NULL OR octet_length(node->>'source_jcs') NOT BETWEEN 2 AND 1048576
  OR encode(sha256(convert_to(node->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM binding->'source'->>'sha256' THEN RETURN false;END IF;
 source_body:=(node->>'source_jcs')::jsonb;
 IF jsonb_typeof(source_body->'canvas'->'width') IS DISTINCT FROM 'number'
  OR jsonb_typeof(source_body->'canvas'->'height') IS DISTINCT FROM 'number'
  OR coalesce(source_body->'canvas'->>'width','') !~ '^[1-9][0-9]*$'
  OR coalesce(source_body->'canvas'->>'height','') !~ '^[1-9][0-9]*$' THEN RETURN false;END IF;
 width:=(source_body->'canvas'->>'width')::bigint; height:=(source_body->'canvas'->>'height')::bigint;
 IF width>200000000 OR height>200000000/width THEN RETURN false;END IF;
 pixels:=width*height; mapped:=112*pixels+8192*16;
 IF p_profile->>'alpha_mode'='normalize_uint16' THEN mapped:=mapped+4*pixels+8192;END IF;
 tile_pixels:=least(width,(p_profile->>'tile_size')::bigint)*least(height,(p_profile->>'tile_size')::bigint);
 IF (p_profile->'resource_estimate'->>'pixel_count')::bigint<>pixels
  OR (p_profile->'resource_estimate'->>'ram_bytes')::bigint<mapped+256*tile_pixels+268435456
  OR (p_profile->'resource_estimate'->>'scratch_bytes')::bigint<mapped+67108864
  OR (p_profile->'resource_estimate'->>'output_bytes')::bigint<24*pixels+33554432 THEN RETURN false;END IF;
 IF body->>'schema_version' IS DISTINCT FROM '1' OR body->'recipe'->'source' IS DISTINCT FROM binding
  OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR p_profile->>'backend' NOT IN('cpu','cuda','mps') OR p_profile->>'backend' IS NULL
  OR coalesce(p_profile->>'render_runtime_sha','') !~ '^[0-9a-f]{64}$'
  OR p_profile->>'model_lock_sha' IS DISTINCT FROM body->'scalars'->>'model_lock_sha'
  OR p_profile->>'icc_lock_sha' IS DISTINCT FROM body->'scalars'->>'icc_lock_sha'
  OR coalesce(p_profile->'resource_estimate'->>'output_bytes','') !~ '^[0-9]+$'
  OR (p_profile->'resource_estimate'->>'output_bytes')::bigint>reserved THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.v26_render_recipe_scope(p_tenant,w.project_id,version_id,p_params,p_sha,r.created_by,session_id)
  OR NOT EXISTS(SELECT 1 FROM photo.snapshot_items item WHERE item.tenant_id=p_tenant AND item.snapshot_id=r.input_snapshot_id
   AND item.asset_version_id=version_id AND item.artifact_id=(binding->'source'->>'artifact_id')::uuid
   AND item.sha256=binding->'source'->>'sha256') THEN RETURN false;END IF;
 IF NOT photo.v2_draft_artifact_scope(p_tenant,w.project_id,version_id,
  (body->'recipe'->'base_style'->>'artifact_id')::uuid,body->'recipe'->'base_style'->>'sha256',
  'StyleV2',r.created_by,session_id) THEN RETURN false;END IF;
 IF body->'learning'<>'null'::jsonb AND body->'learning' IS NOT NULL THEN
  IF NOT photo.v26_render_model_scope(p_tenant,w.project_id,version_id,p_params,p_sha,
   (body->'learning'->'model'->>'artifact_id')::uuid,body->'learning'->'model'->>'sha256',
   (body->'learning'->>'version_id')::uuid,r.created_by,session_id)
  OR NOT photo.v26_render_descriptor_scope(p_tenant,w.project_id,version_id,p_params,p_sha,
   (body->'learning'->'descriptor'->>'artifact_id')::uuid,body->'learning'->'descriptor'->>'sha256',
   (body->'learning'->>'analysis_id')::uuid,(body->'learning'->>'ordinal')::integer,
   body->'scalars'->>'model_lock_sha',r.created_by,session_id) THEN RETURN false;END IF;
 END IF;
 class:=CASE WHEN p_profile->>'backend'='cpu' THEN 'cpu' ELSE 'gpu' END;
 key:='render.compose:'||p_ordinal::text;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=key AND old.operation='render.compose'
  AND old.operation_version='1' AND old.resource_class=class AND old.params_artifact_id=p_params
  AND old.input_digest=p_digest AND (old.execution_profile=p_profile OR (photo.v2_input_closure_ready(old.id)
   AND (old.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=p_profile))
  AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,
  inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(p_task,p_tenant,p_run,key,'render.compose','1',class,'blocked','[]'::jsonb,p_params,p_digest,p_profile);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v26_render_recipe_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v26_render_recipe_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v26_render_recipe_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
