-- +goose Up
-- Current scheduler evidence snapshot; no IO or adoption mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_adoption_evidence(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 body jsonb;plan jsonb;params_sha text;source jsonb;previous_tenant text;
 image jsonb;arrays jsonb:='[]'::jsonb;location jsonb;ref jsonb;role text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 body:=p_body::jsonb;params_sha:=encode(sha256(convert_to(p_body,'UTF8')),'hex');
 SELECT * INTO c FROM photo.v2_adoption_context(p_tenant,p_task);
 IF NOT FOUND OR c.attempt IS DISTINCT FROM p_attempt OR c.fence IS DISTINCT FROM p_fence
 OR c.result_id IS DISTINCT FROM p_result OR c.operation IS DISTINCT FROM 'optical.perspective_lines'
 OR c.operation_version IS DISTINCT FROM '1' THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id FOR SHARE;
 plan:=r.config_snapshot->'v29_perspective_line_detection';
 IF r.v29_perspective_line_detection IS DISTINCT FROM true OR r.graph_sealed IS DISTINCT FROM true
 OR r.v29_perspective_lines_seal_receipt IS NULL OR r.v29_perspective_lines_execution_policy IS NULL
 OR plan->>'task_id' IS DISTINCT FROM p_task::text OR plan->>'params_jcs' IS DISTINCT FROM p_body
 OR body IS DISTINCT FROM jsonb_build_object('schema_version',1,'base',plan->'base')
 OR w.id IS NULL OR w.workflow_type<>'preview' OR w.input_snapshot_id<>r.input_snapshot_id
 OR w.policy_sha IS DISTINCT FROM encode(sha256(convert_to(r.v29_perspective_lines_execution_policy,'UTF8')),'hex')
 OR t.execution_profile->>'execution_policy_sha' IS DISTINCT FROM w.policy_sha
 OR t.execution_profile->>'runtime_lock_sha' IS DISTINCT FROM (r.v29_perspective_lines_execution_policy::jsonb)->>'runtime_lock_sha'
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' OR t.execution_profile->>'precision' IS DISTINCT FROM 'float32'
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM i.artifact_id FROM photo.v2_task_inputs i JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=t.params_artifact_id
 AND i.contract='PerspectiveLineDetectionParams' AND i.sha256=params_sha AND a.state='ready'
 AND a.output_role='task_params' AND a.verified_at IS NOT NULL AND a.bytes=octet_length(p_body)
 AND a.media_type='application/json' AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
 AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence AND review_required) THEN RETURN NULL;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 source:=photo.v29_perspective_source_document(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
 (plan->>'auth_session_id')::uuid,plan->'base');
 image:=(plan->>'source_jcs')::jsonb;
 FOR role,ref IN SELECT x.role,x.ref FROM (VALUES
 (0,'WorkingPixels'::text,image->'pixels'),(1,'WorkingAlpha'::text,image->'alpha')) x(ordinal,role,ref)
 WHERE x.ref IS NOT NULL AND x.ref<>'null'::jsonb ORDER BY x.ordinal LOOP
  location:=photo.v29_perspective_array_source(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
   (plan->>'auth_session_id')::uuid,plan->'base',plan->>'source_jcs',(ref->>'artifact_id')::uuid,ref->>'sha256',role);
  IF location IS NULL THEN arrays:=NULL;EXIT;END IF;
  arrays:=arrays||jsonb_build_array(jsonb_build_object('role',role,'object',location));
 END LOOP;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF arrays IS NULL OR arrays IS DISTINCT FROM r.v29_perspective_lines_seal_receipt->'witness'->'arrays' THEN RETURN NULL;END IF;
 IF source IS NULL OR source IS DISTINCT FROM r.v29_perspective_lines_seal_receipt->'witness'->'source' THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'run_id',r.id,'task_id',p_task,'attempt',p_attempt,'fence',p_fence,
 'result_id',p_result,'params_ref',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha),
 'root',jsonb_build_object('artifact_id',c.root_artifact_id,'sha256',c.root_sha256),
 'params',body,'profile',t.execution_profile,'policy_jcs',r.v29_perspective_lines_execution_policy,'source',source,'arrays',arrays);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 IF previous_tenant IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous_tenant,true);END IF;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_perspective_lines_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
