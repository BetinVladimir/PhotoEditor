-- +goose Up
-- Completed-task original Params and current source/qualification snapshot.
-- No object IO; caller verifies original bytes and independently replays pixels.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_adoption_evidence(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE context record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 body jsonb;pin jsonb;locations jsonb;params_sha text;previous_tenant text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 body:=p_body::jsonb;params_sha:=encode(sha256(convert_to(p_body,'UTF8')),'hex');
 SELECT * INTO context FROM photo.v2_adoption_context(p_tenant,p_task);
 IF NOT FOUND OR context.attempt IS DISTINCT FROM p_attempt OR context.fence IS DISTINCT FROM p_fence
 OR context.result_id IS DISTINCT FROM p_result OR context.operation IS DISTINCT FROM 'optical.negative'
 OR context.operation_version IS DISTINCT FROM '1' THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id FOR SHARE;
 IF t.state IS DISTINCT FROM 'succeeded' OR t.cancel_requested IS DISTINCT FROM false
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.canonical_result_id IS DISTINCT FROM p_result OR t.resource_class IS DISTINCT FROM 'cpu'
 OR r.kind IS DISTINCT FROM 'v2' OR r.graph_sealed IS DISTINCT FROM true OR r.cancel_requested IS DISTINCT FROM false
 OR w.id IS NULL OR w.workflow_type IS DISTINCT FROM 'preview' OR w.renderer_version IS DISTINCT FROM 'v2.2'
 OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='Params'
 AND i.artifact_id=t.params_artifact_id AND i.sha256=params_sha AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes=octet_length(p_body)
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 pin:=t.execution_profile->'negative_qualification';
 IF pin->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'negative_runtime_sha'
 OR coalesce(t.execution_profile->>'negative_runtime_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
 (r.config_snapshot->'v29_negative'->>'auth_session_id')::uuid,body->'base') IS DISTINCT FROM true THEN
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN NULL;END IF;
 locations:=photo.v29_negative_qualification_sources(p_tenant,w.project_id,r.created_by,
 (r.config_snapshot->'v29_negative'->>'auth_session_id')::uuid,pin);
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF locations IS NULL OR jsonb_array_length(locations) IS DISTINCT FROM 2
 OR body->'calibration' IS DISTINCT FROM pin->'calibration' THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(locations) item WHERE NOT EXISTS(
 SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.contract=item->>'contract' AND i.artifact_id::text=item->>'artifact_id' AND i.sha256=item->>'sha256')) THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.contract='WorkingImage' AND i.artifact_id::text=body->'base'->'source'->>'artifact_id'
 AND i.sha256=body->'base'->'source'->>'sha256') THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
 AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence AND review_required) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'run_id',r.id,'task_id',t.id,'attempt',p_attempt,'fence',p_fence,
 'result_id',p_result,'params_ref',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha),
 'params',body,'profile',t.execution_profile,'qualification_objects',locations);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 IF previous_tenant IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous_tenant,true);END IF;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_negative_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_negative_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
