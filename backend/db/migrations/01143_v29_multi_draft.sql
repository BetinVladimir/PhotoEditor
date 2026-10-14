-- +goose Up
-- Preliminary immutable draft only. No tasks, reservations or queue transitions.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v29_multi_draft(p_tenant uuid,p_actor uuid,p_session uuid,p_plan text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;params jsonb;source jsonb;profile jsonb;pin jsonb;resources jsonb;config jsonb;
 v_run uuid;v_project uuid;v_snapshot uuid;old photo.runs%ROWTYPE;h bytea;field text;child jsonb;
 expected uuid;i integer;ordinal integer;binding jsonb;source_raw text;operation text;count_frames integer;budget bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 41943040 THEN RETURN false;END IF;
 plan:=p_plan::jsonb;
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object'
 OR NOT(plan ?& ARRAY['tenant_id','project_id','actor_id','auth_session_id','snapshot_id','operation','run_id','workflow_id','task_id','params_document_id','params_jcs','params_sha','sources_jcs','execution_profile','reserved_bytes'])
 OR (plan-'tenant_id'-'project_id'-'actor_id'-'auth_session_id'-'snapshot_id'-'operation'-'run_id'-'workflow_id'-'task_id'-'params_document_id'-'params_jcs'-'params_sha'-'sources_jcs'-'execution_profile'-'reserved_bytes')<>'{}'::jsonb
 OR plan->>'tenant_id' IS DISTINCT FROM p_tenant::text OR plan->>'actor_id' IS DISTINCT FROM p_actor::text
 OR plan->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR jsonb_typeof(plan->'params_jcs') IS DISTINCT FROM 'string' OR jsonb_typeof(plan->'sources_jcs') IS DISTINCT FROM 'array'
 OR octet_length(plan->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(plan->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM plan->>'params_sha' THEN RETURN false;END IF;
 params:=(plan->>'params_jcs')::jsonb;operation:=plan->>'operation';
 IF operation IS NULL OR operation NOT IN ('optical.panorama','optical.focus_stack') OR jsonb_typeof(params->'frames') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 count_frames:=jsonb_array_length(params->'frames');
 IF count_frames NOT BETWEEN 2 AND CASE operation WHEN 'optical.panorama' THEN 20 ELSE 30 END OR jsonb_array_length(plan->'sources_jcs')<>count_frames THEN RETURN false;END IF;
 profile:=plan->'execution_profile';pin:=profile->'multi_qualification';resources:=profile->'resource_estimate';
 v_run:=(plan->>'run_id')::uuid;v_project:=(plan->>'project_id')::uuid;v_snapshot:=(plan->>'snapshot_id')::uuid;
 IF v_run IS NULL OR v_project IS NULL OR v_snapshot IS NULL
 OR v_run='00000000-0000-0000-0000-000000000000'::uuid
 OR params->>'schema_version' IS DISTINCT FROM '1'
 OR profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR profile->>'multi_runtime_sha' IS DISTINCT FROM pin->>'runtime_lock_sha'
 OR pin->'calibration' IS DISTINCT FROM params->'calibration' THEN RETURN false;END IF;
 -- Deterministic child identities cannot alias input roots or foreign owners.
 h:=public.digest(uuid_send(v_run)||convert_to('workflow','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM (plan->>'workflow_id')::uuid THEN RETURN false;END IF;
 h:=public.digest(uuid_send(expected)||convert_to(operation||':0','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM (plan->>'task_id')::uuid THEN RETURN false;END IF;
 h:=public.digest(uuid_send(expected)||convert_to('params','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM (plan->>'params_document_id')::uuid THEN RETURN false;END IF;
 -- Preliminary profile bounds only. Final graph seal must independently
 -- reconstruct exact resource requirements; saving cannot make a task runnable.
 FOR field IN SELECT unnest(ARRAY['pixel_count','ram_bytes','scratch_bytes','output_bytes']) LOOP
  IF jsonb_typeof(resources->field) IS DISTINCT FROM 'number' OR coalesce(resources->>field,'') !~ '^[0-9]+$' THEN RETURN false;END IF;
  budget:=(resources->>field)::bigint;IF budget NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 END LOOP;
 IF resources->'vram_bytes' IS DISTINCT FROM '0'::jsonb OR (plan->>'reserved_bytes')::bigint IS DISTINCT FROM (resources->>'output_bytes')::bigint THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF photo.lock_v29_multi_qualification(p_tenant,v_project,p_actor,p_session,pin,operation) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR ordinal IN 0..count_frames-1 LOOP
  binding:=params->'frames'->ordinal;
  IF jsonb_typeof(plan->'sources_jcs'->ordinal) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
  source_raw:=plan->'sources_jcs'->>ordinal;
  IF octet_length(source_raw) NOT BETWEEN 2 AND 1048576 OR encode(sha256(convert_to(source_raw,'UTF8')),'hex') IS DISTINCT FROM binding->'source'->>'sha256' THEN RETURN false;END IF;
  source:=source_raw::jsonb;
  IF source->>'dtype' IS DISTINCT FROM 'float32' OR photo.v28_local_portrait_source_scope(p_tenant,v_project,v_snapshot,p_actor,p_session,binding) IS DISTINCT FROM true THEN RETURN false;END IF;
  FOR field,child IN SELECT 'WorkingPixels',source->'pixels' UNION ALL SELECT 'WorkingAlpha',source->'alpha' LOOP
   IF child IS NULL OR child='null'::jsonb THEN IF field='WorkingPixels' THEN RETURN false;END IF;CONTINUE;END IF;
   IF photo.lock_v29_perspective_source_child(p_tenant,v_project,v_snapshot,p_actor,p_session,binding,source_raw,(child->>'artifact_id')::uuid,child->>'sha256',field) IS DISTINCT FROM true THEN RETURN false;END IF;
  END LOOP;
  IF jsonb_typeof(source->'transform_chain'->'steps') IS DISTINCT FROM 'array' OR jsonb_array_length(source->'transform_chain'->'steps')>128 THEN RETURN false;END IF;
  FOR child,i IN SELECT value,(ordinality-1)::integer FROM jsonb_array_elements(source->'transform_chain'->'steps') WITH ORDINALITY LOOP
   IF child->>'kind'='dense_warp' AND photo.lock_v29_perspective_source_child(p_tenant,v_project,v_snapshot,p_actor,p_session,binding,source_raw,(child->'inverse_field'->>'artifact_id')::uuid,child->'inverse_field'->>'sha256','InverseField/'||i) IS DISTINCT FROM true THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 config:=jsonb_build_object('project_id',v_project,'v29_multi_plan',plan);
 SELECT * INTO old FROM photo.runs WHERE id=v_run FOR UPDATE;
 IF old.id IS NOT NULL THEN RETURN old.tenant_id=p_tenant AND old.created_by=p_actor AND old.kind='v2' AND old.status='draft' AND NOT old.graph_sealed AND NOT old.cancel_requested AND old.input_snapshot_id=v_snapshot AND old.config_snapshot=config;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(v_run,p_tenant,'v2','draft',v_snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES((plan->>'workflow_id')::uuid,p_tenant,v_project,v_run,v_snapshot,'preview','v2.2',pin->>'policy_sha');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v29_multi_draft(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_multi_draft(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v29_multi_draft(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
