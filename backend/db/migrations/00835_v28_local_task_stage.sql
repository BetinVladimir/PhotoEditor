-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v28_local_task_plan jsonb;
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_inventory_digest_ready(p_task uuid,p_params uuid,p_params_sha text,p_operation text,p_input_digest text,p_entries jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; ref jsonb; aid uuid; prior uuid; contract_name text; body text:='['; first_item boolean:=true; params_count integer:=0;
BEGIN
 IF p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid OR p_params IS NULL
 OR p_params='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_input_digest,'') !~ '^[0-9a-f]{64}$'
 OR p_operation IS NULL OR p_operation NOT IN('portrait.hair','portrait.clothing') OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_entries) NOT BETWEEN 1 AND 127 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_entries) LOOP
  ref:=item->'ref';contract_name:=item->>'contract';
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR NOT(item ?& ARRAY['ref','contract'])
  OR (item-'ref'-'contract')<>'{}'::jsonb OR jsonb_typeof(item->'contract') IS DISTINCT FROM 'string'
  OR contract_name NOT IN('Params','WorkingImage','MaskRef','RasterizedMask','ManualMask','MaskPixels','NpyArray','Artifact')
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR (prior IS NOT NULL AND aid<=prior) THEN RETURN false;END IF;
  IF aid=p_params THEN
   params_count:=params_count+1;
   IF contract_name<>'Params' OR ref->>'sha256'<>p_params_sha THEN RETURN false;END IF;
  END IF;
  IF NOT first_item THEN body:=body||',';END IF;
  body:=body||'{"contract":"'||contract_name||'","ref":{"artifact_id":"'||aid::text||'","sha256":"'||(ref->>'sha256')||'"}}';
  first_item:=false;prior:=aid;
 END LOOP;
 body:=body||']';
 RETURN params_count=1 AND encode(sha256(convert_to('{"entries":'||body||',"operation":"'||p_operation||'","task":"'||p_task::text||'","version":"1"}','UTF8')),'hex')=p_input_digest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_local_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v28_local_portrait_task(p_tenant uuid,p_run uuid,p_plan_jcs text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;old photo.tasks%ROWTYPE;plan jsonb;task jsonb;intent jsonb;manual jsonb;
 task_id uuid;params_id uuid;sha text;operation text;
BEGIN
 IF p_plan_jcs IS NULL OR octet_length(p_plan_jcs) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 plan:=p_plan_jcs::jsonb;task:=plan->'task';intent:=r.config_snapshot->'v28_local_portrait'->'intent';
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object' OR plan->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(plan) k) IS DISTINCT FROM ARRAY['binding','entries','params_jcs','reserved_bytes','schema_version','task']::text[]
  OR jsonb_typeof(task) IS DISTINCT FROM 'object'
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(task) k) IS DISTINCT FROM ARRAY['execution_profile','id','input_digest','node_key','operation','operation_version','params_artifact_id','params_sha','resource_class']::text[]
  OR plan->'binding' IS DISTINCT FROM intent->'binding' OR plan->'params_jcs' IS DISTINCT FROM intent->'params_jcs'
  OR plan->'reserved_bytes' IS DISTINCT FROM intent->'reserved_bytes'
  OR task->'id' IS DISTINCT FROM intent->'task_id' OR task->'node_key' IS DISTINCT FROM intent->'node_key'
  OR task->'operation' IS DISTINCT FROM intent->'operation' OR task->'operation_version' IS DISTINCT FROM '"1"'::jsonb
  OR task->'resource_class' IS DISTINCT FROM '"cpu"'::jsonb OR task->'params_sha' IS DISTINCT FROM intent->'params_sha'
  OR task->'execution_profile' IS DISTINCT FROM intent->'profile' THEN RETURN false;END IF;
 task_id:=(task->>'id')::uuid;params_id:=(task->>'params_artifact_id')::uuid;sha:=task->>'params_sha';operation:=task->>'operation';
 IF photo.v28_local_params_document_scope(p_tenant,p_run,task_id,'task_params',sha,octet_length(plan->>'params_jcs')) IS DISTINCT FROM true
  OR photo.v28_local_inventory_digest_ready(task_id,params_id,sha,operation,task->>'input_digest',plan->'entries') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT jsonb_agg(value ORDER BY ordinal) INTO manual FROM jsonb_array_elements(plan->'entries') WITH ORDINALITY entries(value,ordinal)
  WHERE value->'ref'->>'artifact_id'<>params_id::text;
 IF manual IS DISTINCT FROM intent->'manual_entries' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=task_id AND d.purpose='task_params'
   AND d.artifact_id=params_id AND d.state='ready' AND d.sha256=sha AND d.bytes=octet_length(plan->>'params_jcs')
   AND a.state='ready' AND a.sha256=sha AND a.bytes=d.bytes AND a.media_type='application/json'
   AND a.verified_at IS NOT NULL AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF r.v28_local_task_plan IS NOT NULL AND r.v28_local_task_plan IS DISTINCT FROM plan THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=task_id FOR UPDATE;
 IF FOUND THEN RETURN r.v28_local_task_plan=plan AND old.run_id=p_run AND old.node_key=task->>'node_key'
  AND old.operation=operation AND old.operation_version='1' AND old.resource_class='cpu' AND old.params_artifact_id=params_id
  AND old.input_digest=task->>'input_digest' AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested
  AND (old.execution_profile=task->'execution_profile' OR (photo.v2_input_closure_ready(old.id)
   AND (old.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=task->'execution_profile'));END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,p_run,task->>'node_key',operation,'1','cpu','blocked','[]'::jsonb,params_id,task->>'input_digest',task->'execution_profile');
 UPDATE photo.runs SET v28_local_task_plan=plan WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v28_local_portrait_task(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v28_local_portrait_task(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v28_local_portrait_task(uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
