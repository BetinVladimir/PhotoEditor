-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_negative_recovery_source(p_tenant uuid,p_plan jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE params jsonb:=(p_plan->>'params_jcs')::jsonb;image jsonb:=(p_plan->>'source_jcs')::jsonb;edge jsonb;i integer;role text;
BEGIN
 IF encode(sha256(convert_to(p_plan->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM params->'base'->'source'->>'sha256' THEN RETURN false;END IF;
 FOR role,edge IN SELECT 'WorkingPixels',image->'pixels' UNION ALL SELECT 'WorkingAlpha',image->'alpha' LOOP
 IF edge IS NULL OR edge='null'::jsonb THEN IF role='WorkingPixels' THEN RETURN false;END IF;CONTINUE;END IF;
 IF photo.lock_v29_negative_source_child(p_tenant,(p_plan->>'project_id')::uuid,(p_plan->>'snapshot_id')::uuid,(p_plan->>'actor_id')::uuid,(p_plan->>'auth_session_id')::uuid,params->'base',p_plan->>'source_jcs',(edge->>'artifact_id')::uuid,edge->>'sha256',role) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 IF jsonb_typeof(image->'transform_chain'->'steps') IS DISTINCT FROM 'array' OR jsonb_array_length(image->'transform_chain'->'steps')>128 THEN RETURN false;END IF;
 FOR edge,i IN SELECT value,(ordinality-1)::integer FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY LOOP
 IF edge->>'kind'='dense_warp' AND photo.lock_v29_negative_source_child(p_tenant,(p_plan->>'project_id')::uuid,(p_plan->>'snapshot_id')::uuid,(p_plan->>'actor_id')::uuid,(p_plan->>'auth_session_id')::uuid,params->'base',p_plan->>'source_jcs',(edge->'inverse_field'->>'artifact_id')::uuid,edge->'inverse_field'->>'sha256','InverseField/'||i) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_negative_recovery_source(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_negative_recovery_source(uuid,jsonb) FROM PUBLIC;
-- Invoked only by the SECURITY DEFINER receipt reader.
-- +goose StatementEnd
-- Historical admission acknowledgement only; never an execution authority.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_queued_receipt(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_expected text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;receipt jsonb;plan jsonb;request jsonb;actual jsonb;matched integer;expected integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 IF p_expected IS NULL OR octet_length(p_expected) NOT BETWEEN 2 AND 3145728 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
 AND kind='v2' AND graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v29_negative_queued_receipt IS NULL OR r.v29_negative_descriptor_jcs IS NULL OR r.config_snapshot->>'v29_negative_draft_jcs' IS NULL THEN RETURN NULL;END IF;
 receipt:=r.v29_negative_queued_receipt;plan:=r.v29_negative_descriptor_jcs::jsonb;request:=((r.config_snapshot->>'v29_negative_draft_jcs')::jsonb->'owner') || ((((r.config_snapshot->>'v29_negative_draft_jcs')::jsonb->>'recovery_jcs')::jsonb->>'descriptor_jcs')::jsonb);
 request:=request||jsonb_build_object('execution_profile',(request->>'profile_jcs')::jsonb);
 IF p_expected IS NULL OR r.config_snapshot->>'v29_negative_draft_jcs' IS DISTINCT FROM p_expected THEN RETURN NULL;END IF;
 IF receipt->'schema_version' IS DISTINCT FROM '1'::jsonb OR receipt->>'state' IS DISTINCT FROM 'queued'
 OR receipt->>'run_id' IS DISTINCT FROM p_run::text OR receipt->>'actor_id' IS DISTINCT FROM p_actor::text
 OR receipt->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR request->>'actor_id' IS DISTINCT FROM p_actor::text OR request->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR receipt->>'operation' IS DISTINCT FROM 'optical.negative'
 OR receipt->>'task_id' IS DISTINCT FROM plan->'task'->>'id'
 OR receipt->'qualification' IS DISTINCT FROM request->'execution_profile'->'negative_qualification'
 OR receipt->>'descriptor_sha' IS DISTINCT FROM encode(sha256(convert_to(r.v29_negative_descriptor_jcs,'UTF8')),'hex') THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR receipt->>'workflow_id' IS DISTINCT FROM w.id::text
 OR photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,p_actor,p_session,(request->>'params_jcs')::jsonb->'base') IS DISTINCT FROM true
 OR photo.lock_v29_negative_qualification(p_tenant,w.project_id,p_actor,p_session,request->'execution_profile'->'negative_qualification') IS DISTINCT FROM true
 OR photo.lock_v29_negative_recovery_source(p_tenant,request) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(receipt->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.cancel_requested OR t.operation IS DISTINCT FROM 'optical.negative' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.params_artifact_id::text IS DISTINCT FROM plan->'task'->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM plan->'task'->>'input_digest'
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM plan->'task'->'execution_profile'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true
 OR t.execution_profile->'v2_input_manifest' IS DISTINCT FROM receipt->'transport'->'manifest'
 OR t.execution_profile->'v2_entry_count' IS DISTINCT FROM receipt->'transport'->'count'
 OR t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM receipt->'transport'->>'sha256' THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',contract,'ref',jsonb_build_object('artifact_id',artifact_id,'sha256',sha256)) ORDER BY ordinal)
 INTO actual FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id AND contract NOT IN('InputClosureChunk','InputClosureManifest');
 IF actual IS DISTINCT FROM plan->'entries' THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) ORDER BY ordinal) INTO actual
 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id AND contract='InputClosureChunk';
 IF actual IS DISTINCT FROM receipt->'transport'->'chunks' THEN RETURN NULL;END IF;
 SELECT count(*) INTO expected FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id;
 PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.bytes>0
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY a.id FOR SHARE OF a;
 GET DIAGNOSTICS matched=ROW_COUNT;
 IF matched<>expected THEN RETURN NULL;END IF;
 RETURN receipt;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_negative_queued_receipt(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_queued_receipt(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_queued_receipt(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
