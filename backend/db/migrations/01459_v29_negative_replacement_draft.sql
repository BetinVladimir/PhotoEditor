-- +goose Up
-- Immutable preliminary replacement draft. No tasks, queue or output reservation.
-- Physical input and qualification verification remains required by admission.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v29_negative_replacement_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_draft text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE draft jsonb;identity jsonb;owner jsonb;marker jsonb;recovery jsonb;descriptor jsonb;params jsonb;source jsonb;profile jsonb;pin jsonb;
 namespace uuid;new_task uuid;new_run uuid;workflow uuid;original uuid;snapshot uuid;intent jsonb;config jsonb;old photo.runs%ROWTYPE;
 field text;identity_jcs text;marker_jcs text;roots jsonb;physical jsonb;entries jsonb;encoded_entries text;input_jcs text;has_alpha boolean;child jsonb;item jsonb;ref jsonb;
BEGIN
 IF p_draft IS NULL OR octet_length(p_draft) NOT BETWEEN 2 AND 3145728
 OR photo.lock_v29_negative_replacement_scope(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan) IS DISTINCT FROM true THEN RETURN false;END IF;
 draft:=p_draft::jsonb;identity:=draft->'identity';owner:=draft->'owner';
 IF draft->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(draft) k) IS DISTINCT FROM ARRAY['identity','identity_sha','input_digest','marker_jcs','owner','recovery_jcs','reserved_bytes','schema_version']::text[]
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(identity) k) IS DISTINCT FROM ARRAY['decision_hash','marker_sha','operation','original_run_id','original_task_id','params_document_id','params_sha','project_id','review_id','run_id','schema_version','task_id','tenant_id','workflow_id']::text[]
 OR identity->'schema_version' IS DISTINCT FROM '2'::jsonb OR identity->>'operation' IS DISTINCT FROM 'optical.negative'
 OR identity->>'tenant_id' IS DISTINCT FROM p_tenant::text OR identity->>'project_id' IS DISTINCT FROM p_project::text
 OR identity->>'review_id' IS DISTINCT FROM p_review::text OR identity->>'decision_hash' IS DISTINCT FROM p_hash
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(owner) k) IS DISTINCT FROM ARRAY['actor_id','auth_session_id','project_id','run_id','snapshot_id','task_id','tenant_id','workflow_id']::text[]
 OR owner->>'tenant_id' IS DISTINCT FROM p_tenant::text OR owner->>'project_id' IS DISTINCT FROM p_project::text
 OR owner->>'actor_id' IS DISTINCT FROM p_actor::text OR owner->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN false;END IF;
 FOR field IN SELECT jsonb_object_keys(owner) LOOP
 IF jsonb_typeof(owner->field) IS DISTINCT FROM 'string' OR (owner->>field)::uuid IS NULL
 OR (owner->>field)::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 END LOOP;
 FOR field IN SELECT jsonb_object_keys(identity) LOOP
 IF field<>'schema_version' AND jsonb_typeof(identity->field) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 END LOOP;
 FOR field IN SELECT unnest(ARRAY['identity_sha','input_digest']) LOOP
 IF coalesce(draft->>field,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF coalesce(identity->>'params_sha','') !~ '^[0-9a-f]{64}$' OR coalesce(identity->>'marker_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 original:=(identity->>'original_task_id')::uuid;snapshot:=(owner->>'snapshot_id')::uuid;
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,original);
 IF intent IS NULL OR intent->>'operation' IS DISTINCT FROM 'optical.negative'
 OR identity->>'original_run_id' IS DISTINCT FROM intent->>'original_run_id'
 OR photo.lock_v29_negative_replacement_snapshot(p_tenant,p_project,p_actor,p_session,p_review,p_hash,original,snapshot) IS DISTINCT FROM true THEN RETURN false;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||p_project::text||':'||p_review::text||':'||p_hash);
 new_task:=photo.v30_uuid5(namespace,'task:'||original::text);
 new_run:=photo.v30_uuid5(namespace,'run:'||(intent->>'original_run_id')||':'||original::text);
 workflow:=photo.v30_uuid5(new_run,'workflow');
 IF identity->>'task_id' IS DISTINCT FROM new_task::text OR identity->>'run_id' IS DISTINCT FROM new_run::text
 OR identity->>'workflow_id' IS DISTINCT FROM workflow::text
 OR identity->>'params_document_id' IS DISTINCT FROM photo.v30_uuid5(new_task,'params')::text
 OR owner->>'task_id' IS DISTINCT FROM new_task::text OR owner->>'run_id' IS DISTINCT FROM new_run::text
 OR owner->>'workflow_id' IS DISTINCT FROM workflow::text THEN RETURN false;END IF;
 marker:=jsonb_build_object('version','v29_negative_replacement_v2','project_id',p_project::text,'review_id',p_review::text,
 'decision_hash',p_hash,'original_run_id',intent->>'original_run_id','original_task_id',original::text);
 SELECT '{'||string_agg(to_jsonb(key)::text||':'||value::text,',' ORDER BY key COLLATE "C")||'}' INTO marker_jcs FROM jsonb_each(marker);
 SELECT '{'||string_agg(to_jsonb(key)::text||':'||value::text,',' ORDER BY key COLLATE "C")||'}' INTO identity_jcs FROM jsonb_each(identity);
 IF draft->>'marker_jcs' IS DISTINCT FROM marker_jcs
 OR encode(sha256(convert_to(marker_jcs,'UTF8')),'hex') IS DISTINCT FROM identity->>'marker_sha'
 OR encode(sha256(convert_to(identity_jcs,'UTF8')),'hex') IS DISTINCT FROM draft->>'identity_sha'
 OR jsonb_typeof(draft->'recovery_jcs') IS DISTINCT FROM 'string' OR octet_length(draft->>'recovery_jcs') NOT BETWEEN 2 AND 2097152 THEN RETURN false;END IF;
 recovery:=(draft->>'recovery_jcs')::jsonb;
 IF recovery->'schema_version' IS DISTINCT FROM '1'::jsonb OR recovery->'owner' IS DISTINCT FROM owner
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(recovery) k) IS DISTINCT FROM ARRAY['descriptor_jcs','owner','schema_version']::text[] THEN RETURN false;END IF;
 IF jsonb_typeof(recovery->'descriptor_jcs') IS DISTINCT FROM 'string'
 OR octet_length(recovery->>'descriptor_jcs') NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 descriptor:=(recovery->>'descriptor_jcs')::jsonb;
 IF descriptor->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(descriptor) k) IS DISTINCT FROM ARRAY['params_jcs','params_ref','profile_jcs','reserved_bytes','schema_version','source_jcs']::text[]
 OR descriptor->'params_ref' IS DISTINCT FROM jsonb_build_object('artifact_id',identity->>'params_document_id','sha256',identity->>'params_sha')
 OR descriptor->'reserved_bytes' IS DISTINCT FROM draft->'reserved_bytes' THEN RETURN false;END IF;
 FOR field IN SELECT unnest(ARRAY['params_jcs','source_jcs','profile_jcs']) LOOP
 IF jsonb_typeof(descriptor->field) IS DISTINCT FROM 'string' OR octet_length(descriptor->>field) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 END LOOP;
 params:=(descriptor->>'params_jcs')::jsonb;source:=(descriptor->>'source_jcs')::jsonb;profile:=(descriptor->>'profile_jcs')::jsonb;pin:=profile->'negative_qualification';
 IF params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(params) k) IS DISTINCT FROM ARRAY['base','calibration','exposure_ev','film_base_rgb','schema_version','strength']::text[]
 OR jsonb_typeof(params->'strength') IS DISTINCT FROM 'number' OR (params->>'strength')::numeric NOT BETWEEN 0 AND 1
 OR jsonb_typeof(params->'exposure_ev') IS DISTINCT FROM 'number' OR (params->>'exposure_ev')::numeric NOT BETWEEN -3 AND 3
 OR jsonb_typeof(params->'film_base_rgb') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(params->'film_base_rgb')<>3 THEN RETURN false;END IF;
 FOR child IN SELECT value FROM jsonb_array_elements(params->'film_base_rgb') LOOP
 IF jsonb_typeof(child) IS DISTINCT FROM 'number' OR (child#>>'{}')::numeric NOT BETWEEN 0.000001 AND 1 THEN RETURN false;END IF;
 END LOOP;
 IF encode(sha256(convert_to(descriptor->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM identity->>'params_sha'
 OR encode(sha256(convert_to(descriptor->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM params->'base'->'source'->>'sha256'
 OR params->'calibration' IS DISTINCT FROM pin->'calibration'
 OR source->>'dtype' IS DISTINCT FROM 'float32' OR source->'color'->>'encoding' IS DISTINCT FROM 'linear'
 OR photo.v29_negative_profile_shape((source->'canvas'->>'width')::bigint,(source->'canvas'->>'height')::bigint,
 source->'alpha' IS NOT NULL AND source->'alpha'<>'null'::jsonb,pin->>'runtime_lock_sha',pin,profile) IS DISTINCT FROM true
 OR draft->'reserved_bytes' IS DISTINCT FROM profile->'resource_estimate'->'output_bytes'
 OR photo.lock_v29_negative_qualification(p_tenant,p_project,p_actor,p_session,pin) IS DISTINCT FROM true THEN RETURN false;END IF;
 has_alpha:=source->'alpha' IS NOT NULL AND source->'alpha'<>'null'::jsonb;
 roots:=jsonb_build_array(jsonb_build_object('contract','Params','ref',descriptor->'params_ref'),
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
 input_jcs:='{"entries":['||encoded_entries||'],"operation":"optical.negative","task":'||to_jsonb(new_task::text)::text||',"version":"1"}';
 IF encode(sha256(convert_to(input_jcs,'UTF8')),'hex') IS DISTINCT FROM draft->>'input_digest' THEN RETURN false;END IF;
 config:=jsonb_build_object('project_id',p_project,'v29_negative_replacement',marker,'v29_negative_replacement_draft_jcs',p_draft);
 SELECT * INTO old FROM photo.runs WHERE id=new_run FOR UPDATE;
 IF old.id IS NOT NULL THEN RETURN old.tenant_id=p_tenant AND old.created_by=p_actor AND old.kind='v2' AND old.status='draft'
 AND NOT old.graph_sealed AND NOT old.cancel_requested AND old.input_snapshot_id=snapshot AND old.config_snapshot=config
 AND EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.id=workflow AND w.run_id=new_run
 AND w.project_id=p_project AND w.input_snapshot_id=snapshot AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.policy_sha=pin->>'policy_sha');END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(new_run,p_tenant,'v2','draft',snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(workflow,p_tenant,p_project,new_run,snapshot,'preview','v2.2',pin->>'policy_sha');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v29_negative_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
