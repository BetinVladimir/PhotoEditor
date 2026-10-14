-- +goose Up
-- Blocked task only. Typed closure/IO proof and seal are separate gates.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v29_perspective_line_detection_task(p_tenant uuid,p_run uuid,p_task jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; d photo.backend_documents%ROWTYPE; old photo.tasks%ROWTYPE;
 plan jsonb; image jsonb; inputs jsonb; primary_inputs jsonb; ref jsonb; step jsonb; ticket record;
 task_id uuid; refs_text text:='';digest text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_task IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_task) k) IS DISTINCT FROM ARRAY[
 'execution_profile','id','input_digest','inputs','node_key','operation','operation_version','params','resource_class','timeout_seconds']::text[] THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';task_id:=(p_task->>'id')::uuid;
 IF task_id::text IS DISTINCT FROM plan->>'task_id' THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=task_id AND purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.state<>'ready' THEN RETURN false;END IF;
 SELECT * INTO ticket FROM photo.reserve_v29_perspective_line_detection_document(p_tenant,p_run,task_id,d.sha256,d.bytes,d.bucket,plan->>'params_jcs');
 IF NOT FOUND OR ticket.artifact_id IS DISTINCT FROM d.artifact_id OR ticket.token IS DISTINCT FROM d.token OR ticket.ready IS DISTINCT FROM true THEN RETURN false;END IF;
 image:=(plan->>'source_jcs')::jsonb;
 inputs:=jsonb_build_array(plan->'base'->'source',image->'pixels');
 IF image->'alpha'<>'null'::jsonb THEN inputs:=inputs||jsonb_build_array(image->'alpha');END IF;
 primary_inputs:=inputs;
 IF (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(inputs))<>jsonb_array_length(inputs) THEN RETURN false;END IF;
 FOR step IN SELECT value FROM jsonb_array_elements(image->'transform_chain'->'steps') LOOP
  IF step->>'kind'<>'dense_warp' THEN CONTINUE;END IF;
  ref:=step->'inverse_field';
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(primary_inputs) v WHERE v->>'artifact_id'=ref->>'artifact_id') THEN RETURN false;END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) v WHERE v->>'artifact_id'=ref->>'artifact_id') THEN
   IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) v WHERE v=ref) THEN RETURN false;END IF;
  ELSE inputs:=inputs||jsonb_build_array(ref);END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) v WHERE v->>'artifact_id'=d.artifact_id::text)
 OR p_task->'inputs' IS DISTINCT FROM inputs
 OR p_task->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256)
 OR p_task->>'node_key' IS DISTINCT FROM 'optical.perspective_lines'
 OR p_task->>'operation' IS DISTINCT FROM 'optical.perspective_lines' OR p_task->>'operation_version' IS DISTINCT FROM '1'
 OR p_task->>'resource_class' IS DISTINCT FROM 'cpu' OR p_task->'timeout_seconds' IS DISTINCT FROM '600'::jsonb
 OR p_task->'execution_profile' IS DISTINCT FROM (plan->>'profile_jcs')::jsonb
 OR NOT EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation='optical.perspective_lines' AND version='1' AND resource_class='cpu' AND timeout_seconds=600)
 THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(inputs) LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  IF refs_text<>'' THEN refs_text:=refs_text||',';END IF;
  refs_text:=refs_text||format('{"artifact_id":"%s","sha256":"%s"}',(ref->>'artifact_id')::uuid,ref->>'sha256');
 END LOOP;
 digest:=encode(sha256(convert_to(format('{"inputs":[%s],"operation":"optical.perspective_lines","operation_version":"1","params":{"artifact_id":"%s","sha256":"%s"},"version":"task_input_v1"}',refs_text,d.artifact_id,d.sha256),'UTF8')),'hex');
 IF p_task->>'input_digest' IS DISTINCT FROM digest THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=task_id FOR UPDATE;
 IF FOUND THEN RETURN old.state='blocked' AND NOT old.cancel_requested AND old.canonical_result_id IS NULL
 AND old.run_id=p_run AND old.node_key='optical.perspective_lines' AND old.operation='optical.perspective_lines'
 AND old.operation_version='1' AND old.resource_class='cpu' AND old.params_artifact_id=d.artifact_id
 AND old.inputs=inputs AND old.input_digest=digest AND old.execution_profile=p_task->'execution_profile';END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,p_run,'optical.perspective_lines','optical.perspective_lines','1','cpu','blocked',inputs,d.artifact_id,digest,p_task->'execution_profile');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.materialize_v29_perspective_line_detection_task(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v29_perspective_line_detection_task(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.materialize_v29_perspective_line_detection_task(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
