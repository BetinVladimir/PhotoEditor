-- +goose Up
-- Preliminary analysis draft only: no tasks, reservation, publication or queue.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v28_relight_analysis_draft(p_tenant uuid,p_actor uuid,p_session uuid,p_plan text,p_config text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;config jsonb;request jsonb;profile jsonb;policy jsonb;v_run uuid;v_workflow uuid;v_project uuid;v_snapshot uuid;old photo.runs%ROWTYPE;matched integer;h bytea;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 262144
 OR p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 524288 THEN RETURN false;END IF;
 plan:=p_plan::jsonb;profile:=plan->'execution_profile';
 -- config is emitted as a JSON object by the compiler, not a JSON string.
 request:=plan->'config';
 v_run:=(plan->>'run_id')::uuid;v_workflow:=(plan->>'workflow_id')::uuid;
 v_project:=(request->>'project_id')::uuid;v_snapshot:=(request->>'snapshot_id')::uuid;
 IF request->>'tenant_id' IS DISTINCT FROM p_tenant::text OR request->>'actor_id' IS DISTINCT FROM p_actor::text
 OR request->>'auth_session_id' IS DISTINCT FROM p_session::text OR request->>'operation' IS DISTINCT FROM plan->>'operation'
 OR plan->>'operation' NOT IN('depth.predict','mask.person') OR request->'execution_profile' IS DISTINCT FROM profile
 OR request->'inputs' IS DISTINCT FROM plan->'inputs' OR request->>'params_sha' IS DISTINCT FROM plan->>'params_sha'
 OR coalesce(plan->>'params_sha','') !~ '^[0-9a-f]{64}$' OR v_run IS NULL OR v_workflow IS NULL OR v_project IS NULL OR v_snapshot IS NULL
 OR jsonb_typeof(plan->'inputs') IS DISTINCT FROM 'array' OR jsonb_array_length(plan->'inputs') NOT BETWEEN 4 AND 64
 OR (plan->>'reserved_bytes')::bigint NOT BETWEEN 8388609 AND 9007199254740991 THEN RETURN false;END IF;
 IF plan->'params' IS DISTINCT FROM CASE plan->>'operation'
 WHEN 'depth.predict' THEN jsonb_build_object('image',request->'base'->'source','model_id','depth_anything_v2_small','model_lock_id',profile->>'model_lock_sha','preprocessing_id','depth_anything_v2_small_518_v1','input_size',518)
 ELSE jsonb_build_object('image',request->'base'->'source','model_id','lraspp_mobilenet_v3_large','model_lock_id',profile->>'model_lock_sha','preprocessing_id','person_guided_v1','long_side',768,'guided_tile',512) END THEN RETURN false;END IF;
 h:=public.digest(uuid_send(v_run)||convert_to('workflow','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 IF v_workflow IS DISTINCT FROM encode(substring(h FROM 1 FOR 16),'hex')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF photo.lock_v28_relight_source(p_tenant,v_project,v_snapshot,p_actor,p_session,request->'base') IS DISTINCT FROM true THEN RETURN false;END IF;
 -- Bind an imported passed report to an enabled matching operator policy.
 SELECT to_jsonb(p) INTO policy FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=v_project AND q.operation=p.operation AND q.operation_version=p.operation_version
 JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id AND a.sha256=q.report_sha
 WHERE p.operation=plan->>'operation' AND p.operation_version='1' AND p.enabled AND p.admission_enabled AND p.license_cleared
 AND p.backend=profile->>'backend' AND p.model_lock_sha=profile->>'model_lock_sha' AND p.runtime_lock_sha=profile->>'runtime_lock_sha' AND p.device_profile_sha=profile->>'device_profile_sha'
 AND q.backend=p.backend AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
 AND q.status='passed' AND q.trusted_import AND a.id=(profile->'qualification'->>'artifact_id')::uuid AND a.sha256=profile->'qualification'->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported' AND log.resource_id=q.id AND log.resource_type='model_qualification' AND log.details->>'policy_id'=p.id::text)
 ORDER BY p.id LIMIT 1 FOR SHARE OF p,q,a;
 IF policy IS NULL THEN RETURN false;END IF;
 config:=jsonb_build_object('project_id',v_project,'v28_analysis_plan',plan,'v28_analysis_policy',policy);
 IF p_config::jsonb IS DISTINCT FROM jsonb_build_object('project_id',v_project,'v28_analysis_plan',plan) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.runs WHERE id=v_run FOR UPDATE;
 IF old.id IS NOT NULL THEN RETURN old.tenant_id=p_tenant AND old.created_by=p_actor AND old.kind='v2' AND old.status='draft' AND NOT old.graph_sealed AND NOT old.cancel_requested AND old.input_snapshot_id=v_snapshot AND old.config_snapshot=config;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(v_run,p_tenant,'v2','draft',v_snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(v_workflow,p_tenant,v_project,v_run,v_snapshot,'preview','v2.2',encode(sha256(convert_to(policy::text,'UTF8')),'hex'));
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v28_relight_analysis_draft(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_relight_analysis_draft(uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_relight_analysis_draft(uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
