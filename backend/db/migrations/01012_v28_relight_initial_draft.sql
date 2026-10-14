-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.create_v28_relight_initial_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_request text,p_pin jsonb,p_config text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;params jsonb;v_run uuid;v_workflow uuid;v_snapshot uuid;config jsonb;old photo.runs%ROWTYPE;h bytea;expected uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 131072 THEN RETURN false;END IF;
 request:=p_request::jsonb;params:=request->'params';
 IF jsonb_typeof(request) IS DISTINCT FROM 'object' OR request->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(request) k) IS DISTINCT FROM ARRAY['ordinal','params','run_id','schema_version','snapshot_id','subject_kind','workflow_id']::text[]
 OR request->>'subject_kind' NOT IN('person','manual') OR jsonb_typeof(request->'ordinal') IS DISTINCT FROM 'number'
 OR (request->>'ordinal')::numeric NOT BETWEEN 0 AND 4999 OR (request->>'ordinal')::numeric<>trunc((request->>'ordinal')::numeric)
 OR jsonb_typeof(params) IS DISTINCT FROM 'object' OR params->'calibration' IS DISTINCT FROM p_pin->'calibration' THEN RETURN false;END IF;
 v_run:=(request->>'run_id')::uuid;v_workflow:=(request->>'workflow_id')::uuid;v_snapshot:=(request->>'snapshot_id')::uuid;
 IF v_run IS NULL OR v_workflow IS NULL OR v_snapshot IS NULL OR v_run='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 h:=public.digest(uuid_send(v_run)||convert_to('workflow','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF v_workflow IS DISTINCT FROM expected THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF photo.lock_v28_relight_source(p_tenant,p_project,v_snapshot,p_actor,p_session,params->'base') IS DISTINCT FROM true
 OR photo.lock_v28_relight_qualification(p_tenant,p_project,p_actor,p_session,p_pin) IS DISTINCT FROM true THEN RETURN false;END IF;
 config:=jsonb_build_object('project_id',p_project,'v28_relight_request',request,'v28_relight_pin',p_pin,'auth_session_id',p_session);
 IF p_config IS NULL OR octet_length(p_config)>262144 OR p_config::jsonb IS DISTINCT FROM config THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.runs WHERE id=v_run FOR UPDATE;
 IF old.id IS NOT NULL THEN
 RETURN old.tenant_id=p_tenant AND old.created_by=p_actor AND old.kind='v2' AND old.status='draft' AND NOT old.graph_sealed AND NOT old.cancel_requested
 AND old.input_snapshot_id=v_snapshot AND old.config_snapshot=config
 AND EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND photo.v2_workflows.run_id=v_run AND id=v_workflow AND project_id=p_project AND input_snapshot_id=v_snapshot AND workflow_type='preview' AND renderer_version='v2.2' AND policy_sha=p_pin->>'policy_sha');
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(v_run,p_tenant,'v2','draft',v_snapshot,config,encode(sha256(convert_to(p_config,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(v_workflow,p_tenant,p_project,v_run,v_snapshot,'preview','v2.2',p_pin->>'policy_sha');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.create_v28_relight_initial_draft(uuid,uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v28_relight_initial_draft(uuid,uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v28_relight_initial_draft(uuid,uuid,uuid,uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.save_v28_relight_intent(uuid,uuid,jsonb) RENAME TO save_v28_relight_intent_pre_initial_draft;
REVOKE ALL ON FUNCTION photo.save_v28_relight_intent_pre_initial_draft(uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.save_v28_relight_intent(p_tenant uuid,p_run uuid,p_request jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE config jsonb;frozen jsonb;
BEGIN
 SELECT config_snapshot INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF config ? 'v28_relight_request' THEN
 frozen:=config->'v28_relight_request';
 IF (p_request->>'params_jcs')::jsonb IS DISTINCT FROM frozen->'params'
 OR p_request->'qualification' IS DISTINCT FROM config->'v28_relight_pin'
 OR p_request->>'auth_session_id' IS DISTINCT FROM config->>'auth_session_id'
 OR p_request->>'node_key' IS DISTINCT FROM 'image.relight:'||(frozen->>'ordinal') THEN RETURN false;END IF;
 END IF;
 RETURN photo.save_v28_relight_intent_pre_initial_draft(p_tenant,p_run,p_request);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v28_relight_intent(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_relight_intent(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_relight_intent(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
