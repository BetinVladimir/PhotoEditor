-- +goose Up
-- Requires02650 exact operation/version qualification, with no table grants.
-- These are lease-bound projections of existing private canonical inventories;
-- no caller-supplied source/profile/phase list becomes authority.
-- +goose StatementBegin
-- Exact prerequisite repair: operation/version/contract-specific authored closure.
DO $closure_guard$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.v2_input_closure_ready(uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.v2_input_closure_ready(uuid)'::regprocedure) IS DISTINCT FROM 'a39294b1d6f3e1a0f5ea72ef21d20e0d96fac7d8ae91666b44f729307eb3fb48' THEN
  RAISE EXCEPTION 'AUTHORED_CLOSURE_PREIMAGE_CHANGED';END IF;
 EXECUTE $closure_definition$CREATE OR REPLACE FUNCTION photo.v2_input_closure_ready(p_task uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE t photo.tasks%ROWTYPE; manifest jsonb; ref jsonb; direct_count integer; entry_count integer; node jsonb; binding jsonb; config jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND OR jsonb_typeof(t.inputs) IS DISTINCT FROM 'array'
  OR (jsonb_typeof(t.inputs)='array' AND jsonb_array_length(t.inputs) NOT BETWEEN 1 AND 128)
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR (coalesce(t.execution_profile->>'contracts_sha','') NOT IN (
   '6c52d2a8828f7370520064925c29bb4a043be05d34934db612e7b1fb9016fad2',
   '341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815',
   '266da073ca9bf6863ac9f9a5f54b00f1ee702a415fd91fa735afea648428e685',
   '4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4') AND NOT (t.operation IN('semantic.transfer','v2.semantic.assemble') AND t.operation_version='1' AND coalesce(t.execution_profile->>'contracts_sha','')='dd261b1a9a7bb7d4775155dc25d6e672f068e6abb5d072bc2ddc5de6279ccb87') AND NOT ((t.operation='render.compose' AND t.operation_version='2' OR t.operation='render.authored_segment' AND t.operation_version='1') AND coalesce(t.execution_profile->>'contracts_sha','')='55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772' OR t.operation='render.authored_geometry' AND t.operation_version='1' AND coalesce(t.execution_profile->>'contracts_sha','')='338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b')) THEN
  RETURN false;
 END IF;
 IF coalesce(t.execution_profile->>'v2_entry_count','') !~ '^[1-9][0-9]{0,6}$'
  OR coalesce(t.execution_profile->>'v2_entries_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 entry_count:=(t.execution_profile->>'v2_entry_count')::integer;
 IF entry_count>1000000 THEN RETURN false; END IF;
 manifest:=t.execution_profile->'v2_input_manifest';
 IF jsonb_typeof(manifest) IS DISTINCT FROM 'object' OR (manifest-'artifact_id'-'sha256')<>'{}'::jsonb
  OR manifest->>'artifact_id' IS NULL OR manifest->>'sha256' IS NULL
  OR manifest->>'sha256' !~ '^[0-9a-f]{64}$' THEN
  RETURN false;
 END IF;
 SELECT count(*) INTO direct_count FROM jsonb_array_elements(t.inputs) AS x(ref)
  WHERE x.ref=manifest;
 IF direct_count<>1 THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='v2'
   AND w.renderer_version='v2.2') THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract='InputClosureManifest' AND i.artifact_id::text=manifest->>'artifact_id'
     AND i.sha256=manifest->>'sha256')<>1 THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract='InputClosureChunk')<>(entry_count+511)/512 THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract NOT IN ('InputClosureManifest','InputClosureChunk'))<>entry_count THEN
  RETURN false;
 END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(ref)<>'object' OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb
   OR ref->>'artifact_id' IS NULL OR ref->>'sha256' IS NULL
   OR ref->>'sha256' !~ '^[0-9a-f]{64}$' THEN
   RETURN false;
  END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.artifact_id::text=ref->>'artifact_id' AND i.sha256=ref->>'sha256') THEN
   RETURN false;
  END IF;
 END LOOP;
 IF t.operation='render.compose' AND NOT (t.operation_version='2' AND coalesce(t.execution_profile->>'contracts_sha','')='55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772') THEN
  IF t.operation_version<>'1' OR entry_count NOT BETWEEN 2 AND 4096
   OR jsonb_array_length(t.inputs)<>2
   OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '6c52d2a8828f7370520064925c29bb4a043be05d34934db612e7b1fb9016fad2' THEN RETURN false;END IF;
  SELECT config_snapshot INTO config FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
  IF jsonb_typeof(config->'v26_render'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT value INTO node FROM jsonb_array_elements(config->'v26_render'->'nodes') WHERE value->>'task_id'=t.id::text;
  IF node IS NULL OR t.params_artifact_id::text IS DISTINCT FROM node->'params'->>'artifact_id'
   OR t.inputs->1 IS DISTINCT FROM node->'params' THEN RETURN false;END IF;
  binding:=(node->>'params_jcs')::jsonb->'scalars'->'source';
  IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id AND contract='FrozenRenderRecipe')<>1
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.contract='FrozenRenderRecipe' AND i.artifact_id=t.params_artifact_id AND i.sha256=node->'params'->>'sha256')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.contract='WorkingImage' AND i.artifact_id::text=binding->'source'->>'artifact_id'
    AND i.sha256=binding->'source'->>'sha256') THEN RETURN false;END IF;
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $function$
$closure_definition$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.v2_input_closure_ready(uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_CLOSURE_METADATA_CHANGED';END IF;
END $closure_guard$;
-- Qualify only the installed base observer's existing counter, fixing42702.
DO $base_guard$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_base_canonical(uuid,uuid,jsonb)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_base_canonical(uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM '04fa9a935d4485ab158e11210b5381962f171a7c5f526afab8d891175bc349c9' THEN
  RAISE EXCEPTION 'AUTHORED_BASE_PREIMAGE_CHANGED';END IF;
 EXECUTE $base_definition$CREATE OR REPLACE FUNCTION photo.authored_render_base_canonical(p_tenant uuid, p_parent uuid, p_body jsonb)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
<<authored_base_validation>>
DECLARE intent photo.authored_render_intents%ROWTYPE;parent photo.runs%ROWTYPE;
 child photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;
 stage jsonb;output jsonb;profile jsonb;expected_task uuid;ordinal integer:=0;seen uuid[]:=ARRAY[]::uuid[];child_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_parent IS NULL OR p_body IS NULL THEN RETURN false;END IF;
 IF jsonb_typeof(p_body) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF p_body->>'schema_version' IS DISTINCT FROM '1'
 OR p_body->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR p_body->>'parent_run_id' IS DISTINCT FROM p_parent::text THEN RETURN false;END IF;
 IF jsonb_typeof(p_body->'stages') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_body->'stages') NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 SELECT * INTO intent FROM photo.authored_render_intents WHERE tenant_id=p_tenant AND parent_run_id=p_parent;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent;
 IF NOT FOUND OR parent.kind<>'v2' OR parent.cancel_requested OR parent.status NOT IN('running','waiting_review') THEN RETURN false;END IF;
 IF parent.config_snapshot#>>'{v26_authored,program_sha}' IS DISTINCT FROM intent.program_sha
 OR parent.config_snapshot->>'project_id' IS DISTINCT FROM intent.project_id::text
 OR parent.config_snapshot#>>'{v26_authored,original_recipe,artifact_id}' IS DISTINCT FROM intent.original_recipe_id::text
 OR parent.config_snapshot#>>'{v26_authored,original_recipe,sha256}' IS DISTINCT FROM intent.original_recipe_sha
 OR p_body#>>'{original_recipe,artifact_id}' IS DISTINCT FROM intent.original_recipe_id::text
 OR p_body#>>'{original_recipe,sha256}' IS DISTINCT FROM intent.original_recipe_sha THEN RETURN false;END IF;
 -- Publication is fresh authority: an earlier accepted child is not sufficient
 -- after actor/session/project/recipe withdrawal. Replay of the immutable API
 -- receipt is separate and deliberately does not re-enter this writer scope.
 IF NOT photo.v2_editor_authorized(p_tenant,intent.project_id,parent.created_by,
   (parent.config_snapshot#>>'{v26_authored,auth_session_id}')::uuid)
 OR NOT photo.v26_render_recipe_scope(p_tenant,intent.project_id,
   (p_body#>>'{original_source,asset_version_id}')::uuid,intent.original_recipe_id,
   intent.original_recipe_sha,parent.created_by,
   (parent.config_snapshot#>>'{v26_authored,auth_session_id}')::uuid)
 THEN RETURN false;END IF;
 child_id:=(p_body->>'base_run_id')::uuid;
 IF child_id IS NULL OR child_id=p_parent THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.authored_render_base_children b WHERE b.tenant_id=p_tenant
  AND b.parent_run_id=p_parent AND b.child_run_id=child_id AND b.project_id=intent.project_id
  AND b.actor_id=parent.created_by AND b.program_sha=intent.program_sha
  AND b.phase_plan_sha=p_body->>'phase_plan_sha256') THEN RETURN false;END IF;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=p_tenant AND id=child_id;
 IF NOT FOUND OR child.kind<>'apply' OR child.cancel_requested OR child.status NOT IN('running','waiting_review','completed')
 OR child.created_by IS DISTINCT FROM parent.created_by
 OR child.config_snapshot->>'project_id' IS DISTINCT FROM intent.project_id::text
 OR child.config_snapshot#>>'{authored_render_origin,parent_run_id}' IS DISTINCT FROM p_parent::text
 OR child.config_snapshot#>>'{authored_render_origin,program_sha}' IS DISTINCT FROM intent.program_sha
 OR child.config_snapshot#>>'{authored_render_origin,phase_plan_sha}' IS DISTINCT FROM p_body->>'phase_plan_sha256'
 OR child.config_snapshot#>'{authored_render_origin,original_source}' IS DISTINCT FROM p_body->'original_source'
 OR child.config_snapshot#>'{authored_render_origin,effective_style}' IS DISTINCT FROM p_body->'effective_style'
 THEN RETURN false;END IF;
 FOR stage IN SELECT value FROM jsonb_array_elements(p_body->'stages') LOOP
  IF jsonb_typeof(stage) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
  IF stage->>'ordinal' IS DISTINCT FROM ordinal::text THEN RETURN false;END IF;
  IF jsonb_typeof(stage->'outputs') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  IF jsonb_array_length(stage->'outputs') NOT BETWEEN 1 AND 128 THEN RETURN false;END IF;
  IF jsonb_typeof(stage->'execution_profile_jcs') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
  IF octet_length(stage->>'execution_profile_jcs') NOT BETWEEN 2 AND 1048576
   OR encode(sha256(convert_to(stage->>'execution_profile_jcs','UTF8')),'hex') IS DISTINCT FROM stage->>'execution_profile_sha256'
  THEN RETURN false;END IF;
  profile:=(stage->>'execution_profile_jcs')::jsonb;
  IF jsonb_typeof(profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=(stage->>'task_id')::uuid;
  IF NOT FOUND OR t.run_id<>child_id OR t.id=ANY(seen) OR t.state<>'succeeded' OR t.cancel_requested
   OR t.operation IS DISTINCT FROM stage->>'operation' OR t.operation='editor.layers_precompose'
   OR t.attempt::text IS DISTINCT FROM stage->>'attempt' OR t.fencing_token::text IS DISTINCT FROM stage->>'fence'
   OR t.input_digest IS DISTINCT FROM stage->>'input_digest' OR t.execution_profile IS DISTINCT FROM profile
   OR t.params_artifact_id::text IS DISTINCT FROM stage#>>'{params,artifact_id}' THEN RETURN false;END IF;
  SELECT n.id INTO expected_task FROM photo.apply_nodes n JOIN photo.apply_v4_stages v
   ON v.tenant_id=n.tenant_id AND v.run_id=n.run_id AND v.phase=n.phase
   WHERE n.tenant_id=p_tenant AND n.run_id=child_id ORDER BY v.stage_index,n.ordinal LIMIT 1 OFFSET authored_base_validation.ordinal;
  IF expected_task IS DISTINCT FROM t.id OR NOT EXISTS(SELECT FROM photo.apply_nodes n
   WHERE n.tenant_id=p_tenant AND n.run_id=child_id AND n.id=t.id
    AND n.phase=stage->>'phase' AND n.operation=t.operation AND n.params_sha256=stage#>>'{params,sha256}')
  THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
    AND a.sha256=stage#>>'{params,sha256}' AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id
   AND id=t.canonical_result_id AND id=(stage->>'result_id')::uuid AND attempt=t.attempt AND fencing_token=t.fencing_token;
  IF NOT FOUND OR result.outputs IS DISTINCT FROM stage->'outputs' THEN RETURN false;END IF;
  FOR output IN SELECT value FROM jsonb_array_elements(stage->'outputs') LOOP
   IF jsonb_typeof(output) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
   IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid
    AND a.sha256=output->>'sha256' AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt
    AND a.produced_fence=t.fencing_token AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  END LOOP;
  seen:=array_append(seen,t.id);ordinal:=ordinal+1;
 END LOOP;
 -- No omitted failed, pending or foreign task may disappear behind the final
 -- image's hash. Persisted review-barrier decisions are additionally required
 -- by the expansion authority; this observer alone never admits a child.
 RETURN (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=child_id)=ordinal;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $function$
$base_definition$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_base_canonical(uuid,uuid,jsonb)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_BASE_METADATA_CHANGED';END IF;
END $base_guard$;
-- The same closed key set; compare in its literal ASCII order, not locale order.
DO $profile_guard_0$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_final_profile_current(uuid,uuid,jsonb)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_final_profile_current(uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM '7e317c4823bcae4833d07db0aebb9d5f11be60cb8e46e30d98090bd6de25c58d' THEN
  RAISE EXCEPTION 'AUTHORED_PROFILE_PREIMAGE_CHANGED';END IF;
 EXECUTE $profile_definition_0$CREATE OR REPLACE FUNCTION photo.authored_render_final_profile_current(p_tenant uuid, p_project uuid, p_profile jsonb)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE pin jsonb;part jsonb;field record;policy photo.v2_qualification_policies%ROWTYPE;
 qualified photo.model_qualifications%ROWTYPE;report photo.artifacts%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(key ORDER BY key COLLATE "C") FROM jsonb_object_keys(p_profile) key) IS DISTINCT FROM
 ARRAY['alpha_mode','authored_kernel_version','backend','contracts_sha','icc_lock_sha','model_lock_sha','native_qualification_pin','operation','operation_version','render_runtime_sha','renderer_version','resource_estimate','retained_stage_peak','review_required','tile_size']::text[] THEN RETURN false;END IF;
 IF p_profile->>'backend' IS DISTINCT FROM 'cpu' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_profile->>'authored_kernel_version' IS DISTINCT FROM 'v2.authored.1'
 OR p_profile->>'operation' IS DISTINCT FROM 'render.compose' OR p_profile->>'operation_version' IS DISTINCT FROM '2'
 OR p_profile->>'contracts_sha' IS DISTINCT FROM '55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772'
 OR p_profile->'review_required' IS DISTINCT FROM 'true'::jsonb
 OR p_profile->'tile_size' NOT IN('256'::jsonb,'512'::jsonb,'1024'::jsonb)
 OR jsonb_typeof(p_profile->'alpha_mode') IS DISTINCT FROM 'string'
 OR p_profile->>'alpha_mode' NOT IN('require_float32','normalize_uint16') THEN RETURN false;END IF;
 FOREACH part IN ARRAY ARRAY[p_profile->'resource_estimate',p_profile->'retained_stage_peak'] LOOP
  IF jsonb_typeof(part) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_profile->'resource_estimate') key)
 IS DISTINCT FROM ARRAY['output_bytes','pixel_count','ram_bytes','scratch_bytes','vram_bytes']::text[]
 OR (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_profile->'retained_stage_peak') key)
 IS DISTINCT FROM ARRAY['output_bytes','ram_bytes','scratch_bytes','vram_bytes']::text[] THEN RETURN false;END IF;
 FOREACH part IN ARRAY ARRAY[p_profile->'resource_estimate',p_profile->'retained_stage_peak'] LOOP
  FOR field IN SELECT * FROM jsonb_each(part) LOOP
   IF jsonb_typeof(field.value) IS DISTINCT FROM 'number' OR field.value::text !~ '^(0|[1-9][0-9]*)$'
    THEN RETURN false;END IF;
   IF (field.value::text)::numeric>9007199254740991 THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 IF p_profile#>>'{resource_estimate,vram_bytes}' IS DISTINCT FROM '0'
 OR p_profile#>>'{retained_stage_peak,vram_bytes}' IS DISTINCT FROM '0'
 OR (p_profile#>>'{resource_estimate,pixel_count}')::numeric NOT BETWEEN 1 AND 200000000
 OR (p_profile#>>'{resource_estimate,ram_bytes}')::numeric<1
 OR (p_profile#>>'{resource_estimate,scratch_bytes}')::numeric<1
 OR (p_profile#>>'{resource_estimate,output_bytes}')::numeric<1 THEN RETURN false;END IF;
 pin:=p_profile->'native_qualification_pin';
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(pin) key) IS DISTINCT FROM
 ARRAY['backend','calibration_sha','dataset_sha','device_profile_sha','model_lock_sha','runtime_lock_sha']::text[] THEN RETURN false;END IF;
 FOR field IN SELECT * FROM jsonb_each(pin-'backend') LOOP
  IF jsonb_typeof(field.value) IS DISTINCT FROM 'string' OR (field.value#>>'{}') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF pin->>'backend' IS DISTINCT FROM 'cpu' OR pin->>'model_lock_sha' IS DISTINCT FROM p_profile->>'model_lock_sha'
 OR pin->>'runtime_lock_sha' IS DISTINCT FROM p_profile->>'render_runtime_sha'
 OR coalesce(p_profile->>'icc_lock_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='render.compose' AND version='2' AND v2_enabled
  AND resource_class='gpu' AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO policy FROM photo.v2_qualification_policies WHERE operation='render.compose' AND operation_version='2'
  AND admission_enabled AND enabled AND license_cleared AND backend='cpu'
  AND model_lock_sha=pin->>'model_lock_sha' AND runtime_lock_sha=pin->>'runtime_lock_sha'
  AND device_profile_sha=pin->>'device_profile_sha' AND dataset_sha=pin->>'dataset_sha' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO qualified FROM photo.model_qualifications WHERE tenant_id=p_tenant AND project_id=p_project
  AND operation=policy.operation AND operation_version=policy.operation_version
  AND model_lock_sha=policy.model_lock_sha AND runtime_lock_sha=policy.runtime_lock_sha
  AND backend=policy.backend AND device_profile_sha=policy.device_profile_sha AND dataset_sha=policy.dataset_sha
  AND trusted_import AND status='passed' ORDER BY id LIMIT 1 FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=qualified.report_artifact_id
  AND sha256=qualified.report_sha AND state='ready' AND media_type='application/json'
  AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN('','null')
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $function$
$profile_definition_0$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_final_profile_current(uuid,uuid,jsonb)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_PROFILE_METADATA_CHANGED';END IF;
END $profile_guard_0$;
-- The same closed key set; compare in its literal ASCII order, not locale order.
DO $profile_guard_1$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_geometry_profile_current(uuid,uuid,jsonb)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_geometry_profile_current(uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM 'd9b10ebe29ddf07e4cc70c2cf4b31c6c8c8d1f354c80db115e29ee3c04706db8' THEN
  RAISE EXCEPTION 'AUTHORED_PROFILE_PREIMAGE_CHANGED';END IF;
 EXECUTE $profile_definition_1$CREATE OR REPLACE FUNCTION photo.authored_render_geometry_profile_current(p_tenant uuid, p_project uuid, p_profile jsonb)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE pin jsonb;part jsonb;field record;policy photo.v2_qualification_policies%ROWTYPE;
 qualified photo.model_qualifications%ROWTYPE;report photo.artifacts%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(key ORDER BY key COLLATE "C") FROM jsonb_object_keys(p_profile) key) IS DISTINCT FROM
 ARRAY['alpha_mode','authored_kernel_version','backend','contracts_sha','icc_lock_sha','model_lock_sha','native_qualification_pin','operation','operation_version','render_runtime_sha','renderer_version','resource_estimate','retained_stage_peak','review_required','tile_size']::text[] THEN RETURN false;END IF;
 IF p_profile->>'backend' IS DISTINCT FROM 'cpu' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_profile->>'authored_kernel_version' IS DISTINCT FROM 'v2.authored.geometry.1'
 OR p_profile->>'operation' IS DISTINCT FROM 'render.authored_geometry' OR p_profile->>'operation_version' IS DISTINCT FROM '1'
 OR p_profile->>'contracts_sha' IS DISTINCT FROM '338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b'
 OR p_profile->'review_required' IS DISTINCT FROM 'true'::jsonb
 OR jsonb_typeof(p_profile->'tile_size') IS DISTINCT FROM 'number'
 OR NOT coalesce(p_profile->'tile_size' IN('256'::jsonb,'512'::jsonb,'1024'::jsonb),false)
 OR jsonb_typeof(p_profile->'alpha_mode') IS DISTINCT FROM 'string'
 OR NOT coalesce(p_profile->>'alpha_mode' IN('require_float32','normalize_uint16'),false) THEN RETURN false;END IF;
 FOREACH part IN ARRAY ARRAY[p_profile->'resource_estimate',p_profile->'retained_stage_peak'] LOOP
  IF jsonb_typeof(part) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_profile->'resource_estimate') key)
 IS DISTINCT FROM ARRAY['output_bytes','pixel_count','ram_bytes','scratch_bytes','vram_bytes']::text[]
 OR (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_profile->'retained_stage_peak') key)
 IS DISTINCT FROM ARRAY['output_bytes','ram_bytes','scratch_bytes','vram_bytes']::text[] THEN RETURN false;END IF;
 FOREACH part IN ARRAY ARRAY[p_profile->'resource_estimate',p_profile->'retained_stage_peak'] LOOP
  FOR field IN SELECT * FROM jsonb_each(part) LOOP
   IF jsonb_typeof(field.value) IS DISTINCT FROM 'number' OR field.value::text !~ '^(0|[1-9][0-9]*)$'
    THEN RETURN false;END IF;
   IF (field.value::text)::numeric>9007199254740991 THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 IF p_profile#>>'{resource_estimate,vram_bytes}' IS DISTINCT FROM '0'
 OR p_profile#>>'{retained_stage_peak,vram_bytes}' IS DISTINCT FROM '0'
 OR (p_profile#>>'{resource_estimate,pixel_count}')::numeric NOT BETWEEN 1 AND 200000000
 OR (p_profile#>>'{resource_estimate,ram_bytes}')::numeric<1
 OR (p_profile#>>'{resource_estimate,scratch_bytes}')::numeric<1
 OR (p_profile#>>'{resource_estimate,output_bytes}')::numeric<1 THEN RETURN false;END IF;
 pin:=p_profile->'native_qualification_pin';
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(pin) key) IS DISTINCT FROM
 ARRAY['backend','calibration_sha','dataset_sha','device_profile_sha','model_lock_sha','runtime_lock_sha']::text[] THEN RETURN false;END IF;
 FOR field IN SELECT * FROM jsonb_each(pin-'backend') LOOP
  IF jsonb_typeof(field.value) IS DISTINCT FROM 'string' OR (field.value#>>'{}') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF pin->>'backend' IS DISTINCT FROM 'cpu' OR pin->>'model_lock_sha' IS DISTINCT FROM p_profile->>'model_lock_sha'
 OR pin->>'runtime_lock_sha' IS DISTINCT FROM p_profile->>'render_runtime_sha'
 OR coalesce(p_profile->>'icc_lock_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='render.authored_geometry' AND version='1' AND v2_enabled
  AND resource_class='gpu' AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO policy FROM photo.v2_qualification_policies WHERE operation='render.authored_geometry' AND operation_version='1'
  AND admission_enabled AND enabled AND license_cleared AND backend='cpu'
  AND model_lock_sha=pin->>'model_lock_sha' AND runtime_lock_sha=pin->>'runtime_lock_sha'
  AND device_profile_sha=pin->>'device_profile_sha' AND dataset_sha=pin->>'dataset_sha' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO qualified FROM photo.model_qualifications WHERE tenant_id=p_tenant AND project_id=p_project
  AND operation=policy.operation AND operation_version=policy.operation_version
  AND model_lock_sha=policy.model_lock_sha AND runtime_lock_sha=policy.runtime_lock_sha
  AND backend=policy.backend AND device_profile_sha=policy.device_profile_sha AND dataset_sha=policy.dataset_sha
  AND trusted_import AND status='passed' ORDER BY id LIMIT 1 FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=qualified.report_artifact_id
  AND sha256=qualified.report_sha AND state='ready' AND media_type='application/json'
  AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN('','null')
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 RETURN FOUND AND EXISTS(
  SELECT FROM photo.authored_geometry_generation_evidence g
  JOIN photo.artifacts evidence ON evidence.tenant_id=g.tenant_id AND evidence.id=g.artifact_id
   AND evidence.sha256=g.sha256 AND evidence.state='ready' AND evidence.verified_at IS NOT NULL
   AND evidence.object_version IS NOT NULL AND evidence.object_version NOT IN('','null')
   AND evidence.bytes=octet_length(g.evidence_jcs) AND evidence.media_type='application/json'
   AND (evidence.expires_at IS NULL OR evidence.expires_at>clock_timestamp())
  WHERE g.tenant_id=p_tenant AND g.project_id=p_project
   AND g.evidence_jcs::jsonb->>'contracts_sha'=p_profile->>'contracts_sha'
   AND g.evidence_jcs::jsonb->>'authored_kernel_version'=p_profile->>'authored_kernel_version'
   AND g.evidence_jcs::jsonb->'qualification'=pin-'calibration_sha'
   AND g.evidence_jcs::jsonb->>'calibration_sha256'=pin->>'calibration_sha'
   AND g.evidence_jcs::jsonb#>>'{qualification_report,artifact_id}'=qualified.report_artifact_id::text
   AND g.evidence_jcs::jsonb#>>'{qualification_report,sha256}'=qualified.report_sha
   AND encode(sha256(convert_to(g.report_jcs,'UTF8')),'hex')=qualified.report_sha
   AND policy.required_checks @> ARRAY['authored_geometry_schema_338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b','authored_geometry_calibration']::text[]
   AND policy.evidence_shas @> ARRAY['338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b',pin->>'calibration_sha']::text[]);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $function$
$profile_definition_1$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_geometry_profile_current(uuid,uuid,jsonb)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_PROFILE_METADATA_CHANGED';END IF;
END $profile_guard_1$;
-- The same closed key set; compare in its literal ASCII order, not locale order.
DO $profile_guard_2$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_segment_profile_current(uuid,uuid,jsonb)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_segment_profile_current(uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM 'eb6413d0a8cbe5fd2fe22df2715dd4657cdddbb42095ee770d94dd8d569760b8' THEN
  RAISE EXCEPTION 'AUTHORED_PROFILE_PREIMAGE_CHANGED';END IF;
 EXECUTE $profile_definition_2$CREATE OR REPLACE FUNCTION photo.authored_render_segment_profile_current(p_tenant uuid, p_project uuid, p_profile jsonb)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE pin jsonb;part jsonb;field record;policy photo.v2_qualification_policies%ROWTYPE;
 qualified photo.model_qualifications%ROWTYPE;report photo.artifacts%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(key ORDER BY key COLLATE "C") FROM jsonb_object_keys(p_profile) key) IS DISTINCT FROM
 ARRAY['alpha_mode','authored_kernel_version','backend','contracts_sha','icc_lock_sha','model_lock_sha','native_qualification_pin','operation','operation_version','render_runtime_sha','renderer_version','resource_estimate','retained_stage_peak','review_required','tile_size']::text[] THEN RETURN false;END IF;
 IF p_profile->>'backend' IS DISTINCT FROM 'cpu' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_profile->>'authored_kernel_version' IS DISTINCT FROM 'v2.authored.segment.1'
 OR p_profile->>'operation' IS DISTINCT FROM 'render.authored_segment' OR p_profile->>'operation_version' IS DISTINCT FROM '1'
 OR p_profile->>'contracts_sha' IS DISTINCT FROM '55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772'
 OR p_profile->'review_required' IS DISTINCT FROM 'true'::jsonb
 OR p_profile->'tile_size' NOT IN('256'::jsonb,'512'::jsonb,'1024'::jsonb)
 OR jsonb_typeof(p_profile->'alpha_mode') IS DISTINCT FROM 'string'
 OR p_profile->>'alpha_mode' NOT IN('require_float32','normalize_uint16') THEN RETURN false;END IF;
 FOREACH part IN ARRAY ARRAY[p_profile->'resource_estimate',p_profile->'retained_stage_peak'] LOOP
  IF jsonb_typeof(part) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_profile->'resource_estimate') key)
 IS DISTINCT FROM ARRAY['output_bytes','pixel_count','ram_bytes','scratch_bytes','vram_bytes']::text[]
 OR (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_profile->'retained_stage_peak') key)
 IS DISTINCT FROM ARRAY['output_bytes','ram_bytes','scratch_bytes','vram_bytes']::text[] THEN RETURN false;END IF;
 FOREACH part IN ARRAY ARRAY[p_profile->'resource_estimate',p_profile->'retained_stage_peak'] LOOP
  FOR field IN SELECT * FROM jsonb_each(part) LOOP
   IF jsonb_typeof(field.value) IS DISTINCT FROM 'number' OR field.value::text !~ '^(0|[1-9][0-9]*)$'
    THEN RETURN false;END IF;
   IF (field.value::text)::numeric>9007199254740991 THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 IF p_profile#>>'{resource_estimate,vram_bytes}' IS DISTINCT FROM '0'
 OR p_profile#>>'{retained_stage_peak,vram_bytes}' IS DISTINCT FROM '0'
 OR (p_profile#>>'{resource_estimate,pixel_count}')::numeric NOT BETWEEN 1 AND 200000000
 OR (p_profile#>>'{resource_estimate,ram_bytes}')::numeric<1
 OR (p_profile#>>'{resource_estimate,scratch_bytes}')::numeric<1
 OR (p_profile#>>'{resource_estimate,output_bytes}')::numeric<1 THEN RETURN false;END IF;
 pin:=p_profile->'native_qualification_pin';
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(pin) key) IS DISTINCT FROM
 ARRAY['backend','calibration_sha','dataset_sha','device_profile_sha','model_lock_sha','runtime_lock_sha']::text[] THEN RETURN false;END IF;
 FOR field IN SELECT * FROM jsonb_each(pin-'backend') LOOP
  IF jsonb_typeof(field.value) IS DISTINCT FROM 'string' OR (field.value#>>'{}') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF pin->>'backend' IS DISTINCT FROM 'cpu' OR pin->>'model_lock_sha' IS DISTINCT FROM p_profile->>'model_lock_sha'
 OR pin->>'runtime_lock_sha' IS DISTINCT FROM p_profile->>'render_runtime_sha'
 OR coalesce(p_profile->>'icc_lock_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='render.authored_segment' AND version='1' AND v2_enabled
  AND resource_class='gpu' AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO policy FROM photo.v2_qualification_policies WHERE operation='render.authored_segment' AND operation_version='1'
  AND admission_enabled AND enabled AND license_cleared AND backend='cpu'
  AND model_lock_sha=pin->>'model_lock_sha' AND runtime_lock_sha=pin->>'runtime_lock_sha'
  AND device_profile_sha=pin->>'device_profile_sha' AND dataset_sha=pin->>'dataset_sha' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO qualified FROM photo.model_qualifications WHERE tenant_id=p_tenant AND project_id=p_project
  AND operation=policy.operation AND operation_version=policy.operation_version
  AND model_lock_sha=policy.model_lock_sha AND runtime_lock_sha=policy.runtime_lock_sha
  AND backend=policy.backend AND device_profile_sha=policy.device_profile_sha AND dataset_sha=policy.dataset_sha
  AND trusted_import AND status='passed' ORDER BY id LIMIT 1 FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=qualified.report_artifact_id
  AND sha256=qualified.report_sha AND state='ready' AND media_type='application/json'
  AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN('','null')
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $function$
$profile_definition_2$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_segment_profile_current(uuid,uuid,jsonb)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_PROFILE_METADATA_CHANGED';END IF;
END $profile_guard_2$;
-- Bind retained ordinal to the pre-existing local counter, not a joined column.
DO $ordinal_guard_0$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_final_task_current(uuid,uuid,uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_final_task_current(uuid,uuid,uuid)'::regprocedure) IS DISTINCT FROM '3fd4077bfebdafc87b690c94ed3bf5facc3593975df8699aa2517d77306c87d7' THEN
  RAISE EXCEPTION 'AUTHORED_TASK_PREIMAGE_CHANGED';END IF;
 EXECUTE $ordinal_definition_0$CREATE OR REPLACE FUNCTION photo.authored_render_final_task_current(p_tenant uuid, p_parent uuid, p_task uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
<<authored_task_validation>>
DECLARE t photo.tasks%ROWTYPE;child photo.runs%ROWTYPE;parent photo.runs%ROWTYPE;
 workflow photo.v2_workflows%ROWTYPE;doc photo.backend_documents%ROWTYPE;
 context jsonb;inventory jsonb;node jsonb;base_profile jsonb;scope jsonb;ordinal integer;
 transport jsonb;effective_profile jsonb;effective_digest text;
 expected_child uuid;expected_workflow uuid;expected_task uuid;h bytea;
BEGIN
 -- The binding gate acquires tenant/usage/parent before task/child locks.
 transport:=photo.authored_native_replay_consumer_transport_current(p_tenant,p_task);
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent FOR SHARE;
 IF NOT FOUND OR parent.cancel_requested OR parent.status<>'running' THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.cancel_requested OR t.operation<>'render.compose' OR t.operation_version<>'2'
 OR t.resource_class<>'gpu' OR t.node_key<>'render.compose:0' THEN RETURN false;END IF;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 IF NOT FOUND OR child.id=p_parent OR child.kind<>'v2' OR child.cancel_requested OR NOT child.graph_sealed
 OR child.status NOT IN('running','waiting_review','completed')
 OR child.created_by IS DISTINCT FROM parent.created_by
 OR child.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=child.id)<>1 THEN RETURN false;END IF;
 node:=child.config_snapshot->'authored_final';
 IF jsonb_typeof(node) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 ordinal:=(node->>'ordinal')::integer;
 IF ordinal IS NULL OR ordinal NOT BETWEEN 1 AND 4999 THEN RETURN false;END IF;
 inventory:=photo.authored_render_final_input_context(p_tenant,p_parent,ordinal);
 IF inventory IS NULL THEN RETURN false;END IF;context:=inventory->'context';
 IF context#>>'{instruction,kind}' IS DISTINCT FROM 'finalize'
 OR child.config_snapshot#>>'{authored_phase,parent_run_id}' IS DISTINCT FROM p_parent::text
 OR child.config_snapshot#>>'{authored_phase,instruction_id}' IS DISTINCT FROM context#>>'{instruction,identity}'
 OR child.config_snapshot#>>'{authored_phase,program_sha256}' IS DISTINCT FROM context->>'program_sha'
 OR child.config_snapshot#>>'{authored_phase,predecessor_sha256}' IS DISTINCT FROM context->>'predecessor_sha'
 OR child.config_snapshot#>'{authored_phase,input_source}' IS DISTINCT FROM context->'source'
 OR child.config_snapshot->>'project_id' IS DISTINCT FROM context->>'project'
 OR node->>'task_id' IS DISTINCT FROM p_task::text
 OR node#>>'{params,artifact_id}' IS DISTINCT FROM t.params_artifact_id::text THEN RETURN false;END IF;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=child.id FOR SHARE;
 IF NOT FOUND OR workflow.workflow_type<>'preview' OR workflow.renderer_version<>'v2.2'
 OR workflow.project_id::text IS DISTINCT FROM context->>'project'
 OR workflow.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id THEN RETURN false;END IF;
 -- Reconstruct the actual compiler namespace independently of the config marker.
 h:=public.digest(uuid_send((context#>>'{instruction,identity}')::uuid)||convert_to('AuthoredFinalRun/v1','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_child:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_child)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_workflow:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_workflow)||convert_to('render.compose:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_task:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF child.id IS DISTINCT FROM expected_child OR workflow.id IS DISTINCT FROM expected_workflow
 OR t.id IS DISTINCT FROM expected_task THEN RETURN false;END IF;
 base_profile:=node->'profile';
 effective_profile:=base_profile;effective_digest:=node->>'input_digest';
 -- This helper proves append-only ready replay roots against the unchanged
 -- blocked consumer snapshot. It never nominates a replacement base policy.
 IF transport IS NOT NULL THEN
  IF jsonb_typeof(transport) IS DISTINCT FROM 'object'
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(transport) k)
    IS DISTINCT FROM ARRAY['base_input_digest','base_profile','base_snapshot','entries_jcs','final_input_digest','final_profile','request_evidence_roots']::text[]
  OR transport->'base_profile' IS DISTINCT FROM base_profile
  OR transport->>'base_input_digest' IS DISTINCT FROM node->>'input_digest'
  OR jsonb_typeof(transport->'final_profile') IS DISTINCT FROM 'object'
  OR jsonb_typeof(transport->'request_evidence_roots') IS DISTINCT FROM 'array'
  OR jsonb_array_length(transport->'request_evidence_roots')<2
  OR coalesce(transport->>'final_input_digest','') !~ '^[0-9a-f]{64}$'
  THEN RETURN false;END IF;
  effective_profile:=transport->'final_profile';effective_digest:=transport->>'final_input_digest';
 END IF;
 -- Always qualify the immutable base. Only the protected transport helper
 -- authorizes the additional declared input-index allocation and schema pin.
 IF photo.authored_render_final_profile_dispatch_current(p_tenant,workflow.project_id,base_profile) IS DISTINCT FROM true
 OR t.input_digest IS DISTINCT FROM effective_digest
 OR (t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM effective_profile
 OR photo.authored_render_final_generation_inputs_current(p_tenant,p_task,base_profile) IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 -- The protected parent Params publication is the actual compiler body, not
 -- merely an artifact sharing a digest. Re-enter its original current scope.
 SELECT * INTO doc FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_parent
 AND owner_id=(context#>>'{instruction,identity}')::uuid AND purpose='task_params'
 AND artifact_id=t.params_artifact_id AND sha256=node#>>'{params,sha256}' AND state='ready' FOR SHARE;
 IF NOT FOUND OR doc.authored_final_params_jcs IS DISTINCT FROM node->>'params_jcs' THEN RETURN false;END IF;
 scope:=photo.authored_render_final_params_body_scope(p_tenant,p_parent,ordinal,doc.authored_final_params_jcs);
 IF scope IS NULL OR scope->>'sha256' IS DISTINCT FROM doc.sha256
 OR (scope->>'bytes')::bigint IS DISTINCT FROM doc.bytes THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN photo.v2_artifact_links retained
 ON retained.tenant_id=a.tenant_id AND retained.artifact_id=a.id AND retained.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id AND a.sha256=doc.sha256
 AND a.bytes=doc.bytes AND a.bucket=doc.bucket AND a.object_version=doc.object_version
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_parent||'/documents/'||a.id
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND retained.workflow_id=(context->>'workflow')::uuid AND retained.project_id=workflow.project_id
 AND retained.purpose='v26_authored_final_params' AND retained.ordinal=authored_task_validation.ordinal FOR SHARE OF a;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $function$
$ordinal_definition_0$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_final_task_current(uuid,uuid,uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_TASK_METADATA_CHANGED';END IF;
END $ordinal_guard_0$;
-- Bind retained ordinal to the pre-existing local counter, not a joined column.
DO $ordinal_guard_1$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_geometry_task_current(uuid,uuid,uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_geometry_task_current(uuid,uuid,uuid)'::regprocedure) IS DISTINCT FROM 'c2c258042a0e51f98e00fd9a649098aa31bff3109f2fb56c73017a8d6819f64d' THEN
  RAISE EXCEPTION 'AUTHORED_TASK_PREIMAGE_CHANGED';END IF;
 EXECUTE $ordinal_definition_1$CREATE OR REPLACE FUNCTION photo.authored_render_geometry_task_current(p_tenant uuid, p_parent uuid, p_task uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
<<authored_task_validation>>
DECLARE t photo.tasks%ROWTYPE;child photo.runs%ROWTYPE;parent photo.runs%ROWTYPE;
 workflow photo.v2_workflows%ROWTYPE;doc photo.backend_documents%ROWTYPE;
 context jsonb;inventory jsonb;node jsonb;base_profile jsonb;scope jsonb;ordinal integer;
 expected_child uuid;expected_workflow uuid;expected_task uuid;h bytea;
BEGIN
 -- Match stage/replay order before any prefix helper acquires parent UPDATE.
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent FOR UPDATE;
 IF NOT FOUND OR parent.cancel_requested OR parent.status<>'running' THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.cancel_requested OR t.operation<>'render.authored_geometry' OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.node_key<>'render.authored_geometry:0' THEN RETURN false;END IF;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 IF NOT FOUND OR child.id=p_parent OR child.kind<>'v2' OR child.cancel_requested OR NOT child.graph_sealed
 OR child.status NOT IN('running','waiting_review','completed')
 OR child.created_by IS DISTINCT FROM parent.created_by
 OR child.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=child.id)<>1 THEN RETURN false;END IF;
 node:=child.config_snapshot->'authored_geometry';
 IF jsonb_typeof(node) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 ordinal:=(node->>'ordinal')::integer;
 IF ordinal IS NULL OR ordinal NOT BETWEEN 1 AND 4998 THEN RETURN false;END IF;
 inventory:=photo.authored_render_geometry_input_context(p_tenant,p_parent,ordinal,node#>'{scope_witnesses,compounds}',node#>>'{scope_witnesses,source_jcs}',node#>>'{scope_witnesses,parallel_jcs}');
 IF inventory IS NULL THEN RETURN false;END IF;context:=inventory->'context';
 IF context#>>'{instruction,kind}' IS DISTINCT FROM 'geometry'
 OR child.config_snapshot#>>'{authored_phase,parent_run_id}' IS DISTINCT FROM p_parent::text
 OR child.config_snapshot#>>'{authored_phase,instruction_id}' IS DISTINCT FROM context#>>'{instruction,identity}'
 OR child.config_snapshot#>>'{authored_phase,program_sha256}' IS DISTINCT FROM context->>'program_sha'
 OR child.config_snapshot#>>'{authored_phase,predecessor_sha256}' IS DISTINCT FROM context->>'predecessor_sha'
 OR child.config_snapshot#>'{authored_phase,input_source}' IS DISTINCT FROM context->'source'
 OR child.config_snapshot->>'project_id' IS DISTINCT FROM context->>'project'
 OR node->>'task_id' IS DISTINCT FROM p_task::text
 OR node#>>'{params,artifact_id}' IS DISTINCT FROM t.params_artifact_id::text
 OR node->>'input_digest' IS DISTINCT FROM t.input_digest THEN RETURN false;END IF;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=child.id FOR SHARE;
 IF NOT FOUND OR workflow.workflow_type<>'preview' OR workflow.renderer_version<>'v2.2'
 OR workflow.project_id::text IS DISTINCT FROM context->>'project'
 OR workflow.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id THEN RETURN false;END IF;
 -- Reconstruct the actual compiler namespace independently of the config marker.
 h:=public.digest(uuid_send((context#>>'{instruction,identity}')::uuid)||convert_to('AuthoredGeometryRun/v1','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_child:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_child)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_workflow:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_workflow)||convert_to('render.authored_geometry:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_task:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF child.id IS DISTINCT FROM expected_child OR workflow.id IS DISTINCT FROM expected_workflow
 OR t.id IS DISTINCT FROM expected_task THEN RETURN false;END IF;
 base_profile:=node->'profile';
 IF photo.authored_render_geometry_profile_current(p_tenant,workflow.project_id,base_profile) IS DISTINCT FROM true
 OR (t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM base_profile
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 -- The protected parent Params publication is the actual compiler body, not
 -- merely an artifact sharing a digest. Re-enter its original current scope.
 SELECT * INTO doc FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_parent
 AND owner_id=(context#>>'{instruction,identity}')::uuid AND purpose='task_params'
 AND artifact_id=t.params_artifact_id AND sha256=node#>>'{params,sha256}' AND state='ready' FOR SHARE;
 IF NOT FOUND OR doc.authored_geometry_params_jcs IS DISTINCT FROM node->>'params_jcs' OR doc.authored_geometry_scope_witnesses IS DISTINCT FROM node->'scope_witnesses' THEN RETURN false;END IF;
 scope:=photo.authored_render_geometry_params_body_scope(p_tenant,p_parent,ordinal,doc.authored_geometry_params_jcs,node->'scope_witnesses');
 IF scope IS NULL OR scope->>'sha256' IS DISTINCT FROM doc.sha256
 OR (scope->>'bytes')::bigint IS DISTINCT FROM doc.bytes THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN photo.v2_artifact_links retained
 ON retained.tenant_id=a.tenant_id AND retained.artifact_id=a.id AND retained.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id AND a.sha256=doc.sha256
 AND a.bytes=doc.bytes AND a.bucket=doc.bucket AND a.object_version=doc.object_version
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_parent||'/documents/'||a.id
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND retained.workflow_id=(context->>'workflow')::uuid AND retained.project_id=workflow.project_id
 AND retained.purpose='v26_authored_geometry_params' AND retained.ordinal=authored_task_validation.ordinal FOR SHARE OF a;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $function$
$ordinal_definition_1$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_geometry_task_current(uuid,uuid,uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_TASK_METADATA_CHANGED';END IF;
END $ordinal_guard_1$;
-- Bind retained ordinal to the pre-existing local counter, not a joined column.
DO $ordinal_guard_2$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_segment_task_current(uuid,uuid,uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_segment_task_current(uuid,uuid,uuid)'::regprocedure) IS DISTINCT FROM 'd0514904c024e59963635de076f8412edac652b5b3c1a3f58d0748660cf0ef03' THEN
  RAISE EXCEPTION 'AUTHORED_TASK_PREIMAGE_CHANGED';END IF;
 EXECUTE $ordinal_definition_2$CREATE OR REPLACE FUNCTION photo.authored_render_segment_task_current(p_tenant uuid, p_parent uuid, p_task uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
<<authored_task_validation>>
DECLARE t photo.tasks%ROWTYPE;child photo.runs%ROWTYPE;parent photo.runs%ROWTYPE;
 workflow photo.v2_workflows%ROWTYPE;doc photo.backend_documents%ROWTYPE;
 context jsonb;inventory jsonb;node jsonb;base_profile jsonb;scope jsonb;ordinal integer;
 expected_child uuid;expected_workflow uuid;expected_task uuid;h bytea;
BEGIN
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent FOR SHARE;
 IF NOT FOUND OR parent.cancel_requested OR parent.status<>'running' THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.cancel_requested OR t.operation<>'render.authored_segment' OR t.operation_version<>'1'
 OR t.resource_class<>'gpu' OR t.node_key<>'render.authored_segment:0' THEN RETURN false;END IF;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 IF NOT FOUND OR child.id=p_parent OR child.kind<>'v2' OR child.cancel_requested OR NOT child.graph_sealed
 OR child.status NOT IN('running','waiting_review','completed')
 OR child.created_by IS DISTINCT FROM parent.created_by
 OR child.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=child.id)<>1 THEN RETURN false;END IF;
 node:=child.config_snapshot->'authored_segment';
 IF jsonb_typeof(node) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 ordinal:=(node->>'ordinal')::integer;
 IF ordinal IS NULL OR ordinal NOT BETWEEN 1 AND 4998 THEN RETURN false;END IF;
 inventory:=photo.authored_render_segment_input_context(p_tenant,p_parent,ordinal);
 IF inventory IS NULL THEN RETURN false;END IF;context:=inventory->'context';
 IF context#>>'{instruction,kind}' IS DISTINCT FROM 'segment'
 OR child.config_snapshot#>>'{authored_phase,parent_run_id}' IS DISTINCT FROM p_parent::text
 OR child.config_snapshot#>>'{authored_phase,instruction_id}' IS DISTINCT FROM context#>>'{instruction,identity}'
 OR child.config_snapshot#>>'{authored_phase,program_sha256}' IS DISTINCT FROM context->>'program_sha'
 OR child.config_snapshot#>>'{authored_phase,predecessor_sha256}' IS DISTINCT FROM context->>'predecessor_sha'
 OR child.config_snapshot#>'{authored_phase,input_source}' IS DISTINCT FROM context->'source'
 OR child.config_snapshot->>'project_id' IS DISTINCT FROM context->>'project'
 OR node->>'task_id' IS DISTINCT FROM p_task::text
 OR node#>>'{params,artifact_id}' IS DISTINCT FROM t.params_artifact_id::text
 OR node->>'input_digest' IS DISTINCT FROM t.input_digest THEN RETURN false;END IF;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=child.id FOR SHARE;
 IF NOT FOUND OR workflow.workflow_type<>'preview' OR workflow.renderer_version<>'v2.2'
 OR workflow.project_id::text IS DISTINCT FROM context->>'project'
 OR workflow.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id THEN RETURN false;END IF;
 -- Reconstruct the actual compiler namespace independently of the config marker.
 h:=public.digest(uuid_send((context#>>'{instruction,identity}')::uuid)||convert_to('AuthoredSegmentRun/v1','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_child:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_child)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_workflow:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_workflow)||convert_to('render.authored_segment:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_task:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF child.id IS DISTINCT FROM expected_child OR workflow.id IS DISTINCT FROM expected_workflow
 OR t.id IS DISTINCT FROM expected_task THEN RETURN false;END IF;
 base_profile:=node->'profile';
 IF NOT photo.authored_render_segment_profile_current(p_tenant,workflow.project_id,base_profile)
 OR (t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM base_profile
 OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 -- The protected parent Params publication is the actual compiler body, not
 -- merely an artifact sharing a digest. Re-enter its original current scope.
 SELECT * INTO doc FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_parent
 AND owner_id=(context#>>'{instruction,identity}')::uuid AND purpose='task_params'
 AND artifact_id=t.params_artifact_id AND sha256=node#>>'{params,sha256}' AND state='ready' FOR SHARE;
 IF NOT FOUND OR doc.authored_segment_params_jcs IS DISTINCT FROM node->>'params_jcs' THEN RETURN false;END IF;
 scope:=photo.authored_render_segment_params_body_scope(p_tenant,p_parent,ordinal,doc.authored_segment_params_jcs);
 IF scope IS NULL OR scope->>'sha256' IS DISTINCT FROM doc.sha256
 OR (scope->>'bytes')::bigint IS DISTINCT FROM doc.bytes THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN photo.v2_artifact_links retained
 ON retained.tenant_id=a.tenant_id AND retained.artifact_id=a.id AND retained.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id AND a.sha256=doc.sha256
 AND a.bytes=doc.bytes AND a.bucket=doc.bucket AND a.object_version=doc.object_version
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_parent||'/documents/'||a.id
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND retained.workflow_id=(context->>'workflow')::uuid AND retained.project_id=workflow.project_id
 AND retained.purpose='v26_authored_segment_params' AND retained.ordinal=authored_task_validation.ordinal FOR SHARE OF a;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $function$
$ordinal_definition_2$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_segment_task_current(uuid,uuid,uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_TASK_METADATA_CHANGED';END IF;
END $ordinal_guard_2$;
-- Existing reserve/complete phase publication uses this one closed purpose.
DO $phase_purpose_guard$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(c)-'oid'-'conbin' INTO before_meta FROM pg_constraint c WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF (SELECT encode(sha256(convert_to(pg_get_expr(conbin,conrelid),'UTF8')),'hex') FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check') IS DISTINCT FROM 'b09215bcbd67442e01e7254a4b0a63c39fa0601cf7d8ffb1a5a587e62cbc78a1' THEN RAISE EXCEPTION 'AUTHORED_PHASE_PURPOSE_PREIMAGE_CHANGED';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((((purpose = ANY (ARRAY['task_params'::text, 'review_selection'::text, 'v2_input_manifest'::text, 'v2_session_plan_params'::text, 'v2_analysis_input_snapshot'::text, 'v2_analysis_context'::text, 'v2_analysis_consents'::text, 'v23_training_manifest'::text, 'v23_training_snapshot'::text, 'v23_fit_params'::text, 'v23_evaluate_params'::text, 'v23_frozen_base'::text, 'v23_frozen_approved'::text, 'v23_zero_profile'::text, 'v24_capture_prepare_params'::text, 'v24_capture_shots'::text, 'v24_capture_consents'::text, 'v24_capture_context'::text, 'v24_capture_assess_params'::text, 'v25_lens_params'::text, 'v25_lens_original_binding'::text, 'v28_donor_confirmation'::text])) OR
CASE
    WHEN (purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'::text) THEN (((SUBSTRING(purpose FROM 16))::integer >= 0) AND ((SUBSTRING(purpose FROM 16))::integer <= 1953))
    ELSE false
END OR
CASE
    WHEN (purpose ~ '^v2_analysis_describe_params_([0-9]|[1-3][0-9])$'::text) THEN true
    ELSE false
END OR (purpose = 'v30_delivery_assembly'::text) OR (purpose ~ '^v30_delivery_slots_[0-9]{3}$'::text) OR (purpose = ANY (ARRAY['v30_delivery_snapshot'::text, 'v30_delivery_preset'::text, 'v30_delivery_policy'::text, 'v30_delivery_input'::text])) OR (purpose = 'v30_interchange_snapshot'::text) OR (purpose = 'v30_interchange_import_plan'::text) OR (purpose = 'v30_interchange_import_evidence'::text) OR (purpose = 'v30_import_prepared'::text) OR (purpose = 'v30_interchange_export_plan'::text) OR (purpose = 'v31_signing_lineage'::text) OR (purpose = 'v31_signing_policy'::text) OR (purpose = 'v30_metadata_projection'::text) OR (purpose = 'v30_metadata_source'::text) OR (purpose = ANY (ARRAY['v30_delivery_signed_closure'::text, 'v30_delivery_signed_envelope'::text])) OR (purpose = 'v23_defect_holdout'::text) OR (purpose = 'v23_dust_holdout'::text) OR (purpose = 'v23_detail_holdout'::text) OR (purpose = 'v30_person_origin_receipt'::text) OR (purpose = 'v23_series_holdout'::text) OR (purpose = 'retouch_adapter_context'::text) OR (purpose = 'v28_glare_source_confirmation'::text) OR (purpose = 'v26_authored_base_receipt'::text) OR (purpose = 'v26_authored_continuation'::text) OR (purpose = 'v26_authored_style_binding'::text) OR (purpose = 'v26_authored_layer_mask'::text) OR (purpose = 'v26_authored_layer_params'::text) OR (purpose = 'v26_authored_current_layers'::text) OR (purpose = 'v26_authored_nondetail_params'::text) OR (purpose = ANY (ARRAY['v26_authored_native_replay_request'::text, 'v26_authored_native_replay_evidence'::text])))) OR (purpose = 'v26_authored_phase_receipt'::text));
 SELECT to_jsonb(c)-'oid'-'conbin' INTO after_meta FROM pg_constraint c WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_PHASE_PURPOSE_METADATA_CHANGED';END IF;
END $phase_purpose_guard$;
-- Bind only the pre-existing consumer ordinal, preserving exact GPU replay gates.
DO $native_consumer_guard$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_native_replay_consumer_ready(uuid,uuid,boolean)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_native_replay_consumer_ready(uuid,uuid,boolean)'::regprocedure) IS DISTINCT FROM 'e950788f83df0b5273698e8bbc29e8d40db4e278e6cce8b956972547bdc075ae' THEN RAISE EXCEPTION 'AUTHORED_CONSUMER_PREIMAGE_CHANGED';END IF;
 EXECUTE $consumer_definition$CREATE OR REPLACE FUNCTION photo.authored_native_replay_consumer_ready(p_tenant uuid, p_consumer uuid, p_require_sealed boolean)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
<<authored_consumer_validation>>
DECLARE consumer photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;node jsonb;parent_id uuid;ordinal integer;
 expected record;entry photo.authored_native_replays%ROWTYPE;s jsonb;seen integer:=0;required integer:=0;request jsonb;
BEGIN
 IF p_tenant IS NULL OR p_consumer IS NULL OR p_require_sealed IS NULL THEN RETURN false;END IF;
 SELECT a.* INTO consumer FROM photo.tasks a WHERE a.tenant_id=p_tenant AND a.id=p_consumer;
 SELECT a.* INTO r FROM photo.runs a WHERE a.tenant_id=p_tenant AND a.id=consumer.run_id;
 IF consumer.id IS NULL OR r.id IS NULL THEN RETURN false;END IF;
 IF NOT (r.config_snapshot ? 'authored_geometry' OR r.config_snapshot ? 'authored_final') THEN RETURN true;END IF;
 node:=coalesce(r.config_snapshot->'authored_geometry',r.config_snapshot->'authored_final');
 parent_id:=(r.config_snapshot#>>'{authored_phase,parent_run_id}')::uuid;ordinal:=(node->>'ordinal')::integer;
 -- Strong order before any prefix publication helper; no SHARE->UPDATE upgrade.
 PERFORM 1 FROM photo.tenants a WHERE a.id=p_tenant AND a.status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tenant_usage a WHERE a.tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.runs a WHERE a.tenant_id=p_tenant AND a.id=parent_id FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF photo.authored_render_instruction_context(p_tenant,parent_id,ordinal) IS NULL THEN RETURN false;END IF;
 -- Derive required GPU occurrences from actual immutable current Params and
 -- retained phase rows, never from the set of requests somebody happened to stage.
 FOR expected IN
  SELECT p.ordinal AS geometry_ordinal,p.receipt_jcs::jsonb->'native' AS native
   FROM photo.authored_render_phases p WHERE p.tenant_id=p_tenant AND p.parent_run_id=parent_id AND p.kind='geometry' AND p.ordinal<authored_consumer_validation.ordinal
  UNION ALL
  SELECT authored_consumer_validation.ordinal,(node->>'params_jcs')::jsonb->'native' WHERE consumer.operation='render.authored_geometry'
 LOOP
  IF expected.native IS NULL OR expected.native='null'::jsonb THEN CONTINUE;END IF;
  IF expected.native#>>'{task,operation}' IS DISTINCT FROM 'image.upscale'
   OR ((expected.native#>>'{task,execution_profile_jcs}')::jsonb)->>'backend' NOT IN ('cuda','mps') THEN CONTINUE;END IF;
  required:=required+1;seen:=0;
  FOR entry IN SELECT a.* FROM photo.authored_native_replays a WHERE a.tenant_id=p_tenant AND a.consumer_task_id=p_consumer
   AND a.parent_run_id=parent_id AND a.geometry_ordinal=expected.geometry_ordinal AND a.state='ready' LOOP
   request:=entry.request_jcs::jsonb;
   IF request->'original' IS DISTINCT FROM expected.native THEN RETURN false;END IF;
   s:=photo.authored_native_replay_evidence_scope(p_tenant,entry.replay_task_id,entry.evidence_jcs);
   IF s IS NULL OR s->'consumer_base_snapshot' IS DISTINCT FROM entry.consumer_base_snapshot
    OR s->>'evidence_sha256' IS DISTINCT FROM entry.evidence_sha256 THEN RETURN false;END IF;
   IF (SELECT count(*) FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
    WHERE d.tenant_id=p_tenant AND d.run_id=entry.replay_run_id AND d.owner_id=entry.replay_task_id AND d.state='ready'
    AND ((d.purpose='v26_authored_native_replay_request' AND d.artifact_id=entry.request_artifact_id AND d.sha256=entry.request_sha256)
     OR (d.purpose='v26_authored_native_replay_evidence' AND d.artifact_id=entry.evidence_artifact_id AND d.sha256=entry.evidence_sha256))
    AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.state='ready'
    AND a.verified_at IS NOT NULL AND a.object_version NOT IN ('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))<>2 THEN RETURN false;END IF;
   IF p_require_sealed AND (NOT r.graph_sealed OR photo.v2_input_closure_ready(p_consumer) IS DISTINCT FROM true
    OR (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_consumer
     AND ((i.artifact_id=entry.request_artifact_id AND i.sha256=entry.request_sha256 AND i.contract='AuthoredNativeReplayRequest')
      OR (i.artifact_id=entry.evidence_artifact_id AND i.sha256=entry.evidence_sha256 AND i.contract='AuthoredNativeReplayEvidence')))<>2) THEN RETURN false;END IF;
   seen:=seen+1;
  END LOOP;
  IF seen<>1 THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT count(*) FROM photo.authored_native_replays a WHERE a.tenant_id=p_tenant AND a.consumer_task_id=p_consumer AND a.state='ready')<>required THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $function$
$consumer_definition$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_native_replay_consumer_ready(uuid,uuid,boolean)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_CONSUMER_METADATA_CHANGED';END IF;
END $native_consumer_guard$;
-- Geometry policy calibration_shas pins calibration evidence, not its canonical document hash.
DO $pair_calibration_guard$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p)-'prosrc' INTO before_meta FROM pg_proc p WHERE oid='photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid)'::regprocedure;
 IF (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid)'::regprocedure) IS DISTINCT FROM '2a6ce774cda84d0c374cfc094a6bcc9bf01bc28dd26c4fe9dadf8cca521473d3' THEN
 RAISE EXCEPTION 'AUTHORED_PAIR_PREIMAGE_CHANGED';END IF;
 EXECUTE $pair_definition$CREATE OR REPLACE FUNCTION photo.authored_render_pair_evidence(p_tenant uuid, p_project uuid, p_operation text, p_version text, p_pin jsonb, p_qualification uuid)
 RETURNS boolean
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
BEGIN
 IF jsonb_typeof(p_pin) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 RETURN (SELECT coalesce(
 ((p_operation='render.compose' AND p_version='2') OR
  (p_operation IN('render.authored_segment','render.authored_geometry') AND p_version='1'))
 AND jsonb_typeof(p_pin)='object'
 AND (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_pin) key)=
  ARRAY['backend','calibration_sha','dataset_sha','device_profile_sha','model_lock_sha','runtime_lock_sha']::text[]
 AND NOT EXISTS(SELECT FROM jsonb_each(p_pin) f WHERE jsonb_typeof(f.value)<>'string')
 AND p_pin->>'backend'='cpu'
 AND p_pin->>'calibration_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'model_lock_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'runtime_lock_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'device_profile_sha' ~ '^[0-9a-f]{64}$'
 AND p_pin->>'dataset_sha' ~ '^[0-9a-f]{64}$'
 AND EXISTS(SELECT FROM photo.tenants t JOIN photo.projects p ON p.tenant_id=t.id
  WHERE t.id=p_tenant AND t.status='active' AND p.id=p_project AND p.archived_at IS NULL)
 AND EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=p_operation AND o.version=p_version
  AND o.v2_enabled AND o.resource_class='gpu' AND o.backends ? 'cpu')
 AND EXISTS(SELECT FROM photo.model_qualifications q
  JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id AND a.sha256=q.report_sha
  WHERE q.tenant_id=p_tenant AND q.project_id=p_project AND q.id=p_qualification
  AND q.operation=p_operation AND q.operation_version=p_version AND q.trusted_import AND q.status='passed'
  AND q.model_lock_sha=p_pin->>'model_lock_sha' AND q.runtime_lock_sha=p_pin->>'runtime_lock_sha'
  AND q.backend=p_pin->>'backend' AND q.device_profile_sha=p_pin->>'device_profile_sha' AND q.dataset_sha=p_pin->>'dataset_sha'
  AND a.state='ready' AND a.media_type='application/json' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND EXISTS(SELECT FROM photo.v2_qualification_policies p WHERE p.operation=q.operation AND p.operation_version=q.operation_version
   AND p.model_lock_sha=q.model_lock_sha AND p.runtime_lock_sha=q.runtime_lock_sha AND p.backend=q.backend
   AND p.device_profile_sha=q.device_profile_sha AND p.dataset_sha=q.dataset_sha
   AND p.enabled AND p.admission_enabled AND p.license_cleared
   AND ((p_operation='render.authored_geometry' AND p_version='1'
    AND p.required_checks @> ARRAY['authored_geometry_schema_338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b','authored_geometry_calibration']::text[]
    AND p.evidence_shas @> ARRAY['338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b',p_pin->>'calibration_sha']::text[])
   OR (p_operation<>'render.authored_geometry' AND (p_pin->>'calibration_sha')=ANY(p.calibration_shas))))),false));
END $function$
$pair_definition$;
 SELECT to_jsonb(p)-'prosrc' INTO after_meta FROM pg_proc p WHERE oid='photo.authored_render_pair_evidence(uuid,uuid,text,text,jsonb,uuid)'::regprocedure;
 IF after_meta IS DISTINCT FROM before_meta THEN RAISE EXCEPTION 'AUTHORED_PAIR_METADATA_CHANGED';END IF;
END $pair_calibration_guard$;
CREATE FUNCTION photo.read_authored_render_segment_worker_scope(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_jcs text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;inventory jsonb;parent uuid;ordinal integer;witnesses jsonb;
BEGIN
 IF photo.authored_render_segment_worker_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NOT TRUE THEN RETURN NULL;END IF;
 IF photo.authored_render_worker_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NULL THEN RETURN NULL;END IF;
 body:=p_jcs::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR jsonb_typeof(body->'ordinal') IS DISTINCT FROM 'number'
 OR (body->>'ordinal') !~ '^[1-9][0-9]*$' THEN RETURN NULL;END IF;
 parent:=(body->>'parent_run_id')::uuid;ordinal:=(body->>'ordinal')::integer;
 IF parent IS NULL OR parent=p_run OR ordinal NOT BETWEEN 1 AND 4998 THEN RETURN NULL;END IF;
 inventory:=photo.authored_render_segment_input_context(p_tenant,parent,ordinal);
 IF inventory IS NULL THEN RETURN NULL;END IF;
 IF photo.authored_render_segment_worker_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NOT TRUE THEN RETURN NULL;END IF;
 IF photo.authored_render_worker_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NULL THEN RETURN NULL;END IF;
 RETURN inventory;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_authored_render_segment_worker_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_authored_render_segment_worker_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
CREATE FUNCTION photo.read_authored_render_geometry_worker_scope(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_jcs text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;inventory jsonb;parent uuid;ordinal integer;witnesses jsonb;
BEGIN
 IF photo.authored_render_geometry_worker_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NOT TRUE THEN RETURN NULL;END IF;
 IF photo.authored_render_worker_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NULL THEN RETURN NULL;END IF;
 body:=p_jcs::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR jsonb_typeof(body->'ordinal') IS DISTINCT FROM 'number'
 OR (body->>'ordinal') !~ '^[1-9][0-9]*$' THEN RETURN NULL;END IF;
 parent:=(body->>'parent_run_id')::uuid;ordinal:=(body->>'ordinal')::integer;
 IF parent IS NULL OR parent=p_run OR ordinal NOT BETWEEN 1 AND 4998 THEN RETURN NULL;END IF;
 SELECT a.authored_geometry_scope_witnesses INTO witnesses FROM photo.backend_documents a
 WHERE a.tenant_id=p_tenant AND a.run_id=parent AND a.owner_id=(body->>'instruction_id')::uuid
 AND a.purpose='task_params' AND a.artifact_id=p_params AND a.sha256=p_sha AND a.state='ready';
 IF NOT FOUND OR jsonb_typeof(witnesses) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 inventory:=photo.authored_render_geometry_input_context(p_tenant,parent,ordinal,
  witnesses->'compounds',witnesses->>'source_jcs',witnesses->>'parallel_jcs');
 IF inventory IS NULL THEN RETURN NULL;END IF;
 IF photo.authored_render_geometry_worker_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NOT TRUE THEN RETURN NULL;END IF;
 IF photo.authored_render_worker_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
  p_params,p_sha,p_jcs,p_digest) IS NULL THEN RETURN NULL;END IF;
 RETURN inventory;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_authored_render_geometry_worker_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_authored_render_geometry_worker_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
DO $$ DECLARE f record;a record;BEGIN
 FOR f IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo'
 AND p.proname IN('read_authored_render_segment_worker_scope','read_authored_render_geometry_worker_scope') LOOP
  FOR a IN SELECT grantee FROM aclexplode((SELECT coalesce(proacl,acldefault('f',proowner)) FROM pg_proc WHERE oid=f.oid))
   WHERE grantee<>0 AND grantee<>(SELECT proowner FROM pg_proc WHERE oid=f.oid) LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f.oid::regprocedure,pg_get_userbyid(a.grantee));
  END LOOP;
 END LOOP;
END $$;
GRANT EXECUTE ON FUNCTION photo.read_authored_render_segment_worker_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
GRANT EXECUTE ON FUNCTION photo.read_authored_render_geometry_worker_scope(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
