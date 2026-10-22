-- +goose Up
-- Lock-only identity/resource gate; no run, workflow, task or quota mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_multi_replacement_identity(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_draft text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE operation text;count_frames integer;draft jsonb;identity jsonb;owner jsonb;marker jsonb;recovery jsonb;descriptor jsonb;params jsonb;source jsonb;profile jsonb;pin jsonb;
 namespace uuid;new_task uuid;new_run uuid;workflow uuid;original uuid;snapshot uuid;intent jsonb;config jsonb;old photo.runs%ROWTYPE;
 i integer;a jsonb;b jsonb;c jsonb;field text;identity_jcs text;marker_jcs text;roots jsonb;physical jsonb;entries jsonb;encoded_entries text;input_jcs text;has_alpha boolean;child jsonb;item jsonb;ref jsonb;
BEGIN
 IF p_draft IS NULL OR octet_length(p_draft) NOT BETWEEN 2 AND 41943040
 OR photo.lock_v29_negative_replacement_scope(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan) IS DISTINCT FROM true THEN RETURN false;END IF;
 draft:=p_draft::jsonb;identity:=draft->'identity';descriptor:=draft->'draft';operation:=identity->>'operation';
 owner:=jsonb_build_object('tenant_id',descriptor->'tenant_id','project_id',descriptor->'project_id',
 'actor_id',descriptor->'actor_id','auth_session_id',descriptor->'auth_session_id','snapshot_id',descriptor->'snapshot_id',
 'run_id',descriptor->'run_id','workflow_id',descriptor->'workflow_id','task_id',descriptor->'task_id');
 IF draft->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(draft) k) IS DISTINCT FROM ARRAY['draft','identity','identity_sha','input_digest','marker_jcs','schema_version']::text[]
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(identity) k) IS DISTINCT FROM ARRAY['decision_hash','marker_sha','operation','original_run_id','original_task_id','params_document_id','params_sha','project_id','review_id','run_id','schema_version','task_id','tenant_id','workflow_id']::text[]
 OR identity->'schema_version' IS DISTINCT FROM '2'::jsonb OR (operation IS NULL OR operation NOT IN('optical.panorama','optical.focus_stack'))
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
 IF intent IS NULL OR intent->>'operation' IS DISTINCT FROM operation
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
 'actor_id','auth_session_id','execution_profile','operation','params_document_id','params_jcs','params_sha',
 'project_id','reserved_bytes','run_id','snapshot_id','sources_jcs','task_id','tenant_id','workflow_id']::text[]
 OR descriptor->>'operation' IS DISTINCT FROM operation
 OR descriptor->>'params_document_id' IS DISTINCT FROM identity->>'params_document_id'
 OR descriptor->>'params_sha' IS DISTINCT FROM identity->>'params_sha'
 OR jsonb_typeof(descriptor->'params_jcs') IS DISTINCT FROM 'string'
 OR octet_length(descriptor->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(descriptor->>'params_jcs','UTF8')),'hex') IS DISTINCT FROM identity->>'params_sha'
 OR jsonb_typeof(descriptor->'sources_jcs') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 params:=(descriptor->>'params_jcs')::jsonb;profile:=descriptor->'execution_profile';pin:=profile->'multi_qualification';
 IF params->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(params->'frames') IS DISTINCT FROM 'array'
 OR (params-'schema_version'-'frames'-'reference_ordinal'-'max_pixels'-'calibration'-'projection'-'motion_policy')<>'{}'::jsonb
 OR jsonb_typeof(params->'reference_ordinal') IS DISTINCT FROM 'number'
 OR jsonb_typeof(params->'max_pixels') IS DISTINCT FROM 'number'
 OR coalesce(params->>'reference_ordinal','') !~ '^[0-9]+$' OR coalesce(params->>'max_pixels','') !~ '^[0-9]+$'
 OR profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR profile->>'multi_runtime_sha' IS DISTINCT FROM pin->>'runtime_lock_sha'
 OR pin->'calibration' IS DISTINCT FROM params->'calibration' THEN RETURN false;END IF;
 IF operation='optical.panorama' THEN
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(params) k) IS DISTINCT FROM ARRAY['calibration','frames','max_pixels','projection','reference_ordinal','schema_version']::text[]
 OR params->>'projection' IS NULL OR params->>'projection' NOT IN('planar','cylindrical') THEN RETURN false;END IF;
 ELSE
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(params) k) IS DISTINCT FROM ARRAY['calibration','frames','max_pixels','motion_policy','reference_ordinal','schema_version']::text[]
 OR params->>'motion_policy' IS NULL OR params->>'motion_policy' NOT IN('review','reject') THEN RETURN false;END IF;
 END IF;
 count_frames:=jsonb_array_length(params->'frames');
 IF count_frames NOT BETWEEN 2 AND (CASE operation WHEN 'optical.panorama' THEN 20 ELSE 30 END)
 OR jsonb_array_length(descriptor->'sources_jcs')<>count_frames
 OR (params->>'reference_ordinal')::integer NOT BETWEEN 0 AND count_frames-1
 OR (params->>'max_pixels')::bigint NOT BETWEEN 1 AND 200000000
 OR (SELECT count(DISTINCT f->>'asset_version_id') FROM jsonb_array_elements(params->'frames') f)<>count_frames
 OR photo.v29_multi_resource_estimate(operation,params,descriptor->'sources_jcs') IS NULL
 OR photo.v29_multi_resource_estimate(operation,params,descriptor->'sources_jcs') IS DISTINCT FROM profile->'resource_estimate'
 OR descriptor->'reserved_bytes' IS DISTINCT FROM profile->'resource_estimate'->'output_bytes'
 OR photo.lock_v29_multi_qualification(p_tenant,p_project,p_actor,p_session,pin,operation) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR i IN 0..count_frames-1 LOOP
 a:=params->'frames'->i;
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(a) k) IS DISTINCT FROM ARRAY['asset_version_id','canvas_hash','source']::text[]
 OR jsonb_typeof(a->'asset_version_id') IS DISTINCT FROM 'string' OR (a->>'asset_version_id')::uuid IS NULL
 OR (a->>'asset_version_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(a->>'canvas_hash','') !~ '^[0-9a-f]{64}$'
 OR NOT EXISTS(SELECT 1 FROM photo.snapshot_items si WHERE si.tenant_id=p_tenant AND si.snapshot_id=snapshot AND si.asset_version_id=(a->>'asset_version_id')::uuid)
 OR jsonb_typeof(descriptor->'sources_jcs'->i) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 source:=(descriptor->'sources_jcs'->>i)::jsonb;
 IF source->>'dtype' IS DISTINCT FROM 'float32' OR source->'color'->>'encoding' IS DISTINCT FROM 'linear' THEN RETURN false;END IF;
 END LOOP;
 -- Identity gate only. Caller must retain complete mapped physical source and
 -- located qualification locks; this function creates no execution authority.
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_multi_replacement_identity(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
