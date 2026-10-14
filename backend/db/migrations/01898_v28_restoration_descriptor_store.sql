-- +goose Up
-- Recovery bytes only. This function does not create or admit a task.
ALTER TABLE photo.runs ADD COLUMN v28_restoration_descriptor_jcs text
 CHECK (v28_restoration_descriptor_jcs IS NULL OR
        octet_length(v28_restoration_descriptor_jcs) BETWEEN 2 AND 1048576);
ALTER TABLE photo.runs ADD COLUMN v28_restoration_session_id uuid;

-- +goose StatementBegin
CREATE FUNCTION photo.save_v28_restoration_descriptor(p_tenant uuid,p_run uuid,
 p_actor uuid,p_session uuid,p_descriptor text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; plan jsonb; body jsonb; task jsonb; pin jsonb;
 origin jsonb; target jsonb; guard jsonb; project_id uuid; original_root uuid;
 original_root_sha text; original_params uuid; original_params_sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;body:=p_params::jsonb;task:=plan->'task';origin:=plan->'origin';
 pin:=task->'execution_profile'->'restoration_qualification';
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object' OR plan->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(plan) k) IS DISTINCT FROM
  ARRAY['entries','origin','params','schema_version','task']::text[]
 OR plan->'params' IS DISTINCT FROM body OR jsonb_typeof(body) IS DISTINCT FROM 'object'
 OR jsonb_typeof(task) IS DISTINCT FROM 'object' OR jsonb_typeof(origin) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(origin) k) IS DISTINCT FROM
  ARRAY['params','protected_pixel','root','target_pixel']::text[]
 OR task->>'operation' NOT IN ('portrait.glare','image.reflection')
 OR task->>'operation_version' IS DISTINCT FROM '1' OR task->>'resource_class' IS DISTINCT FROM 'gpu'
 OR task->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR task->'execution_profile'->>'backend' IS DISTINCT FROM pin->>'backend'
 OR encode(sha256(convert_to(p_params,'UTF8')),'hex') IS DISTINCT FROM task->>'params_sha'
 OR photo.v25_layer_ref_shape(origin->'root') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'params') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'target_pixel') IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(origin->'protected_pixel') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 target:=CASE task->>'operation' WHEN 'portrait.glare' THEN body->'glasses_mask' ELSE body->'mask' END;
 guard:=body->'protected';
 IF photo.v25_layer_ref_shape(target) IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(guard) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed
 OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor THEN RETURN false;END IF;
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
 AND w.run_id=p_run AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND w.input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT o.root_id,o.root_sha,o.params_id,o.params_sha
 INTO original_root,original_root_sha,original_params,original_params_sha
 FROM photo.v28_manual_pair_origin(p_tenant,project_id,r.input_snapshot_id,p_actor,p_session,
 body->'base',(target->>'artifact_id')::uuid,target->>'sha256',
 (guard->>'artifact_id')::uuid,guard->>'sha256',
 (origin->'target_pixel'->>'artifact_id')::uuid,origin->'target_pixel'->>'sha256',
 (origin->'protected_pixel'->>'artifact_id')::uuid,origin->'protected_pixel'->>'sha256') o;
 IF original_root IS NULL OR original_root::text IS DISTINCT FROM origin->'root'->>'artifact_id'
 OR original_root_sha IS DISTINCT FROM origin->'root'->>'sha256'
 OR original_params::text IS DISTINCT FROM origin->'params'->>'artifact_id'
 OR original_params_sha IS DISTINCT FROM origin->'params'->>'sha256'
 OR photo.lock_v28_restoration_qualification(p_tenant,project_id,p_actor,p_session,
 task->>'operation',pin) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.purpose='task_params'
 AND d.state='ready' AND d.artifact_id=(task->>'params_artifact_id')::uuid
 AND d.sha256=task->>'params_sha' AND d.bytes=octet_length(p_params)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256
 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.object_version NOT IN ('','null') AND a.media_type='application/json'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF r.v28_restoration_descriptor_jcs IS NOT NULL THEN
  RETURN r.v28_restoration_descriptor_jcs=p_descriptor
   AND r.v28_restoration_session_id=p_session;END IF;
 UPDATE photo.runs SET v28_restoration_descriptor_jcs=p_descriptor,
  v28_restoration_session_id=p_session
 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v28_restoration_descriptor(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_restoration_descriptor(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_restoration_descriptor(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_restoration_descriptor(p_tenant uuid,p_run uuid,
 p_actor uuid,p_session uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;body jsonb;task jsonb;origin jsonb;
 target jsonb;guard jsonb;project_id uuid;root_id uuid;root_sha text;
 params_id uuid;params_sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
 AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_restoration_descriptor_jcs IS NULL
 OR r.v28_restoration_session_id IS DISTINCT FROM p_session THEN RETURN NULL;END IF;
 plan:=r.v28_restoration_descriptor_jcs::jsonb;body:=plan->'params';task:=plan->'task';origin:=plan->'origin';
 target:=CASE task->>'operation' WHEN 'portrait.glare' THEN body->'glasses_mask' ELSE body->'mask' END;
 guard:=body->'protected';
 SELECT w.project_id INTO project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
 AND w.run_id=p_run AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND w.input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT o.root_id,o.root_sha,o.params_id,o.params_sha INTO root_id,root_sha,params_id,params_sha
 FROM photo.v28_manual_pair_origin(p_tenant,project_id,r.input_snapshot_id,p_actor,p_session,
 body->'base',(target->>'artifact_id')::uuid,target->>'sha256',
 (guard->>'artifact_id')::uuid,guard->>'sha256',
 (origin->'target_pixel'->>'artifact_id')::uuid,origin->'target_pixel'->>'sha256',
 (origin->'protected_pixel'->>'artifact_id')::uuid,origin->'protected_pixel'->>'sha256') o;
 IF root_id IS NULL OR root_id::text IS DISTINCT FROM origin->'root'->>'artifact_id'
 OR root_sha IS DISTINCT FROM origin->'root'->>'sha256'
 OR params_id::text IS DISTINCT FROM origin->'params'->>'artifact_id'
 OR params_sha IS DISTINCT FROM origin->'params'->>'sha256'
 OR photo.lock_v28_restoration_qualification(p_tenant,project_id,p_actor,p_session,
 task->>'operation',task->'execution_profile'->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 RETURN r.v28_restoration_descriptor_jcs;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v28_restoration_descriptor(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_restoration_descriptor(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_restoration_descriptor(uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
