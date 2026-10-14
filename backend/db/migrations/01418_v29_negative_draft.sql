-- +goose Up
-- Preliminary immutable negative draft. No tasks, output reservations or queue.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v29_negative_draft(p_tenant uuid,p_actor uuid,p_session uuid,p_plan text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;owner jsonb;recovery jsonb;saved jsonb;params jsonb;source jsonb;profile jsonb;pin jsonb;
 config jsonb;old photo.runs%ROWTYPE;field text;child jsonb;item jsonb;i integer;h bytea;expected uuid;
 v_run uuid;v_project uuid;v_snapshot uuid;v_task uuid;roots jsonb;physical jsonb:='[]'::jsonb;entries jsonb;
 encoded_entries text;request_jcs text;input_jcs text;has_alpha boolean;ref jsonb;role text;
BEGIN
 IF p_tenant IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 3145728 THEN RETURN false;END IF;
 plan:=p_plan::jsonb;owner:=plan->'owner';
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object' OR plan->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(plan) k) IS DISTINCT FROM ARRAY[
 'input_digest','owner','params_document_id','params_sha','recovery_jcs','request_sha','reserved_bytes','schema_version']::text[]
 OR jsonb_typeof(owner) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(owner) k) IS DISTINCT FROM ARRAY[
 'actor_id','auth_session_id','project_id','run_id','snapshot_id','task_id','tenant_id','workflow_id']::text[]
 OR owner->>'tenant_id' IS DISTINCT FROM p_tenant::text OR owner->>'actor_id' IS DISTINCT FROM p_actor::text
 OR owner->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN false;END IF;
 FOR field IN SELECT jsonb_object_keys(owner) LOOP
 IF jsonb_typeof(owner->field) IS DISTINCT FROM 'string' OR (owner->>field)::uuid IS NULL
 OR (owner->>field)::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 END LOOP;
 FOR field IN SELECT unnest(ARRAY['params_sha','input_digest','request_sha']) LOOP
 IF jsonb_typeof(plan->field) IS DISTINCT FROM 'string' OR coalesce(plan->>field,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF jsonb_typeof(plan->'recovery_jcs') IS DISTINCT FROM 'string' OR octet_length(plan->>'recovery_jcs') NOT BETWEEN 2 AND 2097152 THEN RETURN false;END IF;
 recovery:=(plan->>'recovery_jcs')::jsonb;
 IF jsonb_typeof(recovery) IS DISTINCT FROM 'object' OR recovery->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR recovery->'owner' IS DISTINCT FROM owner
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(recovery) k) IS DISTINCT FROM ARRAY['descriptor_jcs','owner','schema_version']::text[]
 OR jsonb_typeof(recovery->'descriptor_jcs') IS DISTINCT FROM 'string'
 OR octet_length(recovery->>'descriptor_jcs') NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 saved:=(recovery->>'descriptor_jcs')::jsonb;
 IF jsonb_typeof(saved) IS DISTINCT FROM 'object' OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(saved) k) IS DISTINCT FROM ARRAY[
 'params_jcs','params_ref','profile_jcs','reserved_bytes','schema_version','source_jcs']::text[]
 OR saved->'params_ref' IS DISTINCT FROM jsonb_build_object('artifact_id',plan->>'params_document_id','sha256',plan->>'params_sha')
 OR saved->'reserved_bytes' IS DISTINCT FROM plan->'reserved_bytes' THEN RETURN false;END IF;
 FOR field IN SELECT unnest(ARRAY['params_jcs','profile_jcs','source_jcs']) LOOP
 IF jsonb_typeof(saved->field) IS DISTINCT FROM 'string' OR octet_length(saved->>field) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 END LOOP;
 params:=(saved->>'params_jcs')::jsonb;source:=(saved->>'source_jcs')::jsonb;profile:=(saved->>'profile_jcs')::jsonb;
 pin:=profile->'negative_qualification';
 IF encode(sha256(convert_to(saved->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM plan->>'params_sha'
 OR encode(sha256(convert_to(saved->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM params->'base'->'source'->>'sha256'
 OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(params) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(params) k) IS DISTINCT FROM ARRAY[
 'base','calibration','exposure_ev','film_base_rgb','schema_version','strength']::text[]
 OR params->'calibration' IS DISTINCT FROM pin->'calibration'
 OR jsonb_typeof(params->'strength') IS DISTINCT FROM 'number'
 OR (params->>'strength')::numeric NOT BETWEEN 0 AND 1
 OR jsonb_typeof(params->'exposure_ev') IS DISTINCT FROM 'number'
 OR (params->>'exposure_ev')::numeric NOT BETWEEN -3 AND 3
 OR jsonb_typeof(params->'film_base_rgb') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(params->'film_base_rgb')<>3 THEN RETURN false;END IF;
 FOR child IN SELECT value FROM jsonb_array_elements(params->'film_base_rgb') LOOP
 IF jsonb_typeof(child) IS DISTINCT FROM 'number' OR (child#>>'{}')::numeric NOT BETWEEN 0.000001 AND 1 THEN RETURN false;END IF;
 END LOOP;
 v_run:=(owner->>'run_id')::uuid;v_project:=(owner->>'project_id')::uuid;v_snapshot:=(owner->>'snapshot_id')::uuid;v_task:=(owner->>'task_id')::uuid;
 h:=public.digest(uuid_send(v_run)||convert_to('workflow','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM (owner->>'workflow_id')::uuid THEN RETURN false;END IF;
 h:=public.digest(uuid_send(expected)||convert_to('optical.negative:0','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM v_task THEN RETURN false;END IF;
 h:=public.digest(uuid_send(expected)||convert_to('params','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM (plan->>'params_document_id')::uuid OR v_run=v_task OR v_run=(owner->>'workflow_id')::uuid THEN RETURN false;END IF;
 IF source->>'dtype' IS DISTINCT FROM 'float32' OR source->'color'->>'encoding' IS DISTINCT FROM 'linear' THEN RETURN false;END IF;
 has_alpha:=source->'alpha' IS NOT NULL AND source->'alpha'<>'null'::jsonb;
 IF photo.v29_negative_profile_shape((source->'canvas'->>'width')::bigint,(source->'canvas'->>'height')::bigint,has_alpha,
 pin->>'runtime_lock_sha',pin,profile) IS DISTINCT FROM true
 OR plan->'reserved_bytes' IS DISTINCT FROM profile->'resource_estimate'->'output_bytes' THEN RETURN false;END IF;
 roots:=jsonb_build_array(jsonb_build_object('contract','Params','ref',saved->'params_ref'),
 jsonb_build_object('contract','WorkingImage','ref',params->'base'->'source'),
 jsonb_build_object('contract','NegativeCalibration','ref',params->'calibration'),
 jsonb_build_object('contract','QualificationReport','ref',pin->'report'));
 IF (SELECT count(DISTINCT value->'ref'->>'artifact_id') FROM jsonb_array_elements(roots))<>4 THEN RETURN false;END IF;
 physical:=jsonb_build_array(jsonb_build_object('contract','NpyArray','ref',source->'pixels'));
 IF has_alpha THEN physical:=physical||jsonb_build_array(jsonb_build_object('contract','NpyArray','ref',source->'alpha'));END IF;
 IF jsonb_typeof(source->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(source->'transform_chain'->'steps')>128 THEN RETURN false;END IF;
 FOR child IN SELECT value FROM jsonb_array_elements(source->'transform_chain'->'steps') LOOP
 IF child->>'kind'='dense_warp' THEN physical:=physical||jsonb_build_array(jsonb_build_object('contract','NpyArray','ref',child->'inverse_field'));END IF;
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(roots||physical) LOOP
 ref:=item->'ref';
 IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(ref) k) IS DISTINCT FROM ARRAY['artifact_id','sha256']::text[]
 OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR (ref->>'artifact_id')::uuid IS NULL
 OR (ref->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string' OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(physical) a JOIN jsonb_array_elements(roots) b ON a->'ref'->>'artifact_id'=b->'ref'->>'artifact_id')
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(physical) a JOIN jsonb_array_elements(physical) b ON a->'ref'->>'artifact_id'=b->'ref'->>'artifact_id' AND a->'ref' IS DISTINCT FROM b->'ref') THEN RETURN false;END IF;
 SELECT jsonb_agg(value ORDER BY (value->'ref'->>'artifact_id') COLLATE "C") INTO entries FROM (SELECT DISTINCT value FROM jsonb_array_elements(roots||physical)) e;
 -- This bounded ASCII-only encoding is JCS for this exact typed inventory.
 SELECT string_agg('{"contract":'||to_jsonb(value->>'contract')::text||',"ref":{"artifact_id":'||to_jsonb(value->'ref'->>'artifact_id')::text||',"sha256":'||to_jsonb(value->'ref'->>'sha256')::text||'}}',',' ORDER BY (value->'ref'->>'artifact_id') COLLATE "C") INTO encoded_entries FROM jsonb_array_elements(entries);
 input_jcs:='{"entries":['||encoded_entries||'],"operation":"optical.negative","task":'||to_jsonb(v_task::text)::text||',"version":"1"}';
 request_jcs:='{"operation":"optical.negative","params_sha":'||to_jsonb(plan->>'params_sha')::text||',"project_id":'||to_jsonb(v_project::text)::text||',"schema_version":1,"snapshot_id":'||to_jsonb(v_snapshot::text)::text||'}';
 IF encode(sha256(convert_to(input_jcs,'UTF8')),'hex') IS DISTINCT FROM plan->>'input_digest'
 OR encode(sha256(convert_to(request_jcs,'UTF8')),'hex') IS DISTINCT FROM plan->>'request_sha' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF photo.v28_local_portrait_source_scope(p_tenant,v_project,v_snapshot,p_actor,p_session,params->'base') IS DISTINCT FROM true
 OR photo.lock_v29_negative_qualification(p_tenant,v_project,p_actor,p_session,pin) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR role,ref IN SELECT 'WorkingPixels',source->'pixels' UNION ALL SELECT 'WorkingAlpha',source->'alpha' LOOP
 IF ref IS NULL OR ref='null'::jsonb THEN IF role='WorkingPixels' THEN RETURN false;END IF;CONTINUE;END IF;
 IF photo.lock_v29_negative_source_child(p_tenant,v_project,v_snapshot,p_actor,p_session,params->'base',saved->>'source_jcs',(ref->>'artifact_id')::uuid,ref->>'sha256',role) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 FOR child,i IN SELECT value,(ordinality-1)::integer FROM jsonb_array_elements(source->'transform_chain'->'steps') WITH ORDINALITY LOOP
 IF child->>'kind'='dense_warp' AND photo.lock_v29_negative_source_child(p_tenant,v_project,v_snapshot,p_actor,p_session,params->'base',saved->>'source_jcs',(child->'inverse_field'->>'artifact_id')::uuid,child->'inverse_field'->>'sha256','InverseField/'||i) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 config:=jsonb_build_object('project_id',v_project,'v29_negative_draft_jcs',p_plan,'v29_negative',jsonb_build_object('auth_session_id',p_session));
 SELECT * INTO old FROM photo.runs WHERE id=v_run FOR UPDATE;
 IF old.id IS NOT NULL THEN RETURN old.tenant_id=p_tenant AND old.created_by=p_actor AND old.kind='v2' AND old.status='draft' AND NOT old.graph_sealed AND NOT old.cancel_requested AND old.input_snapshot_id=v_snapshot AND old.config_snapshot=config
 AND EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.id=(owner->>'workflow_id')::uuid AND w.run_id=v_run AND w.project_id=v_project AND w.input_snapshot_id=v_snapshot AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.policy_sha=pin->>'policy_sha');END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(v_run,p_tenant,'v2','draft',v_snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES((owner->>'workflow_id')::uuid,p_tenant,v_project,v_run,v_snapshot,'preview','v2.2',pin->>'policy_sha');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v29_negative_draft(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_negative_draft(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v29_negative_draft(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
