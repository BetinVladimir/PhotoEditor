-- +goose Up
-- Immutable automatic replacement draft with dedicated adopted segment authority. No tasks, queue or output reservation.
-- Physical input and qualification verification remains required by admission.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v29_auto_perspective_replacement_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_draft text,p_segments jsonb,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE draft jsonb;identity jsonb;owner jsonb;marker jsonb;recovery jsonb;descriptor jsonb;params jsonb;source jsonb;profile jsonb;pin jsonb;
 namespace uuid;new_task uuid;new_run uuid;workflow uuid;original uuid;snapshot uuid;intent jsonb;config jsonb;old photo.runs%ROWTYPE;
 i integer;a jsonb;b jsonb;c jsonb;field text;identity_jcs text;marker_jcs text;roots jsonb;physical jsonb;entries jsonb;encoded_entries text;input_jcs text;has_alpha boolean;child jsonb;item jsonb;ref jsonb;
BEGIN
 IF p_draft IS NULL OR octet_length(p_draft) NOT BETWEEN 2 AND 3145728
 OR photo.lock_v29_auto_perspective_replacement_identity(p_tenant,p_draft,p_segments,p_bucket) IS DISTINCT FROM true
 OR photo.lock_v29_negative_replacement_scope(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan) IS DISTINCT FROM true THEN RETURN false;END IF;
 draft:=p_draft::jsonb;identity:=draft->'identity';descriptor:=draft->'draft';
 owner:=jsonb_build_object('tenant_id',descriptor->'tenant_id','project_id',descriptor->'project_id',
 'actor_id',descriptor->'actor_id','auth_session_id',descriptor->'auth_session_id','snapshot_id',descriptor->'snapshot_id',
 'run_id',descriptor->'run_id','workflow_id',descriptor->'workflow_id','task_id',descriptor->'task_id');
 IF draft->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(draft) k) IS DISTINCT FROM ARRAY['draft','identity','identity_sha','input_digest','marker_jcs','schema_version']::text[]
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(identity) k) IS DISTINCT FROM ARRAY['decision_hash','marker_sha','operation','original_run_id','original_task_id','params_document_id','params_sha','project_id','review_id','run_id','schema_version','task_id','tenant_id','workflow_id']::text[]
 OR identity->'schema_version' IS DISTINCT FROM '2'::jsonb OR identity->>'operation' IS DISTINCT FROM 'optical.perspective'
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
 IF intent IS NULL OR intent->>'operation' IS DISTINCT FROM 'optical.perspective'
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
 THEN RETURN false;END IF;
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(descriptor) k) IS DISTINCT FROM ARRAY[
 'actor_id','auth_session_id','execution_profile','lines_jcs','max_pixels','params_document_id','params_jcs','params_sha',
 'project_id','reserved_bytes','run_id','snapshot_id','source_jcs','task_id','tenant_id','workflow_id']::text[]
 OR descriptor->>'params_document_id' IS DISTINCT FROM identity->>'params_document_id'
 OR descriptor->>'params_sha' IS DISTINCT FROM identity->>'params_sha' THEN RETURN false;END IF;
 FOR field IN SELECT unnest(ARRAY['params_jcs','source_jcs']) LOOP
 IF jsonb_typeof(descriptor->field) IS DISTINCT FROM 'string' OR octet_length(descriptor->>field) NOT BETWEEN 2 AND CASE WHEN field='source_jcs' THEN 1048576 ELSE 65536 END THEN RETURN false;END IF;
 END LOOP;
 params:=(descriptor->>'params_jcs')::jsonb;source:=(descriptor->>'source_jcs')::jsonb;profile:=descriptor->'execution_profile';pin:=profile->'perspective_qualification';
 IF params->'schema_version' IS DISTINCT FROM '1'::jsonb OR params->>'mode' IS DISTINCT FROM 'auto'
 OR params->'corners' IS DISTINCT FROM 'null'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(params) k) IS DISTINCT FROM ARRAY['base','corners','lines','max_crop_fraction','mode','schema_version','strength']::text[]

 OR jsonb_typeof(params->'strength') IS DISTINCT FROM 'number' OR (params->>'strength')::numeric NOT BETWEEN 0 AND 1
 OR jsonb_typeof(params->'max_crop_fraction') IS DISTINCT FROM 'number' OR (params->>'max_crop_fraction')::numeric NOT BETWEEN 0 AND 1 THEN RETURN false;END IF;
 IF encode(sha256(convert_to(descriptor->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM identity->>'params_sha'
 OR encode(sha256(convert_to(descriptor->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM params->'base'->'source'->>'sha256'
 OR source->>'dtype' IS DISTINCT FROM 'float32' OR source->'color'->>'encoding' IS DISTINCT FROM 'linear'
 OR profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR profile->>'perspective_runtime_sha' IS DISTINCT FROM pin->>'runtime_lock_sha'
 OR source->'canvas'->>'width' IS NULL OR source->'canvas'->>'height' IS NULL
 OR descriptor->>'max_pixels' IS NULL OR profile->'resource_estimate'->>'pixel_count' IS NULL
 OR (source->'canvas'->>'width')::bigint NOT BETWEEN 1 AND 200000000
 OR (source->'canvas'->>'height')::bigint NOT BETWEEN 1 AND 200000000
 OR (source->'canvas'->>'width')::bigint*(source->'canvas'->>'height')::bigint NOT BETWEEN 1 AND 200000000
 OR (descriptor->>'max_pixels')::bigint NOT BETWEEN 1 AND 200000000
 OR (profile->'resource_estimate'->>'pixel_count')::bigint NOT BETWEEN 1 AND (descriptor->>'max_pixels')::bigint
 OR (profile->'resource_estimate'->>'vram_bytes')::bigint IS DISTINCT FROM 0
 OR (profile->'resource_estimate'->>'ram_bytes')::bigint IS DISTINCT FROM
 32*(source->'canvas'->>'width')::bigint*(source->'canvas'->>'height')::bigint+20*(profile->'resource_estimate'->>'pixel_count')::bigint+536870912
 OR (profile->'resource_estimate'->>'scratch_bytes')::bigint IS DISTINCT FROM
 32*(source->'canvas'->>'width')::bigint*(source->'canvas'->>'height')::bigint+20*(profile->'resource_estimate'->>'pixel_count')::bigint+67108864
 OR (profile->'resource_estimate'->>'output_bytes')::bigint IS DISTINCT FROM 20*(profile->'resource_estimate'->>'pixel_count')::bigint+33554432
 OR descriptor->'reserved_bytes' IS DISTINCT FROM profile->'resource_estimate'->'output_bytes'
 OR photo.lock_v29_perspective_qualification(p_tenant,p_project,p_actor,p_session,pin) IS DISTINCT FROM true THEN RETURN false;END IF;
 has_alpha:=source->'alpha' IS NOT NULL AND source->'alpha'<>'null'::jsonb;
 roots:=jsonb_build_array(jsonb_build_object('contract','Params','ref',jsonb_build_object('artifact_id',identity->>'params_document_id','sha256',identity->>'params_sha')),
 jsonb_build_object('contract','WorkingImage','ref',params->'base'->'source'),
 jsonb_build_object('contract','Calibration','ref',pin->'calibration'),
 jsonb_build_object('contract','Report','ref',pin->'report'),
 jsonb_build_object('contract','LineSegments','ref',params->'lines'));
 IF (SELECT count(DISTINCT value->'ref'->>'artifact_id') FROM jsonb_array_elements(roots))<>5 THEN RETURN false;END IF;
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
 input_jcs:='{"entries":['||encoded_entries||'],"operation":"optical.perspective","task":'||to_jsonb(new_task::text)::text||',"version":"1"}';
 IF encode(sha256(convert_to(input_jcs,'UTF8')),'hex') IS DISTINCT FROM draft->>'input_digest' THEN RETURN false;END IF;
 config:=jsonb_build_object('project_id',p_project,'v29_negative_replacement',marker,'v29_negative_replacement_draft_jcs',p_draft,'v29_auto_perspective_segments',p_segments);
 SELECT * INTO old FROM photo.runs WHERE id=new_run FOR UPDATE;
 IF old.id IS NOT NULL THEN RETURN old.tenant_id=p_tenant AND old.created_by=p_actor AND old.kind='v2' AND old.status='draft'
 AND old.config_hash=encode(sha256(convert_to(config::text,'UTF8')),'hex') AND NOT old.graph_sealed AND NOT old.cancel_requested AND old.input_snapshot_id=snapshot AND old.config_snapshot=config
 AND EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.id=workflow AND w.run_id=new_run
 AND w.project_id=p_project AND w.input_snapshot_id=snapshot AND w.workflow_type='preview' AND w.renderer_version='v2.2' AND w.policy_sha=pin->>'policy_sha');END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(new_run,p_tenant,'v2','draft',snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(workflow,p_tenant,p_project,new_run,snapshot,'preview','v2.2',pin->>'policy_sha');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v29_auto_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_auto_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v29_auto_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
