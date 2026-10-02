-- +goose Up
-- Worker support exists; enable admission only after the source compiler and
-- qualification gates are wired, as with session/learning operations.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('capture.assess','1','gpu','gpu.capture_assess',NULL,120,'["cuda","cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
VALUES('capture.assess','1','document','["application/json"]',8388608,1);

-- +goose StatementBegin
CREATE FUNCTION photo.close_capture_assessment(p_tenant uuid,p_job uuid,p_token uuid,
 p_workflow uuid,p_params_jcs text,p_context_jcs text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.capture_assessment_jobs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 t photo.tasks%ROWTYPE; params jsonb; context jsonb;
BEGIN
 IF p_params_jcs IS NULL OR p_context_jcs IS NULL OR octet_length(p_params_jcs)>8388608
  OR octet_length(p_context_jcs)>8388608 THEN RETURN false; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO j FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'leased' OR j.lease_token IS DISTINCT FROM p_token
  OR j.lease_until<=clock_timestamp() THEN RETURN false; END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 IF NOT FOUND OR w.project_id<>j.project_id OR w.workflow_type<>'capture' OR w.renderer_version<>'v2.2' THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id)<>1 THEN RETURN false; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id;
 IF t.operation<>'capture.assess' OR t.operation_version<>'1' THEN RETURN false; END IF;
 params:=p_params_jcs::jsonb;context:=p_context_jcs::jsonb;
 IF params->>'schema_version' IS DISTINCT FROM '1' OR context->>'schema_version' IS DISTINCT FROM '1'
  OR (params->'binding'->>'asset_version_id')::uuid IS DISTINCT FROM j.asset_version_id
  OR (context->>'session_id')::uuid IS DISTINCT FROM j.session_id
  OR params->'policy' IS DISTINCT FROM j.capture_policy
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
    AND a.sha256=encode(sha256(convert_to(p_params_jcs,'UTF8')),'hex'))
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id
    AND i.artifact_id=(params->'context'->>'artifact_id')::uuid
    AND i.sha256=params->'context'->>'sha256'
    AND i.sha256=encode(sha256(convert_to(p_context_jcs,'UTF8')),'hex')) THEN RETURN false; END IF;
 IF NOT photo.seal_v2_graph(p_tenant,w.run_id) THEN RETURN false; END IF;
 -- Sealing can wait on other locks: check the fence again before committing.
 UPDATE photo.capture_assessment_jobs SET state='queued',workflow_id=p_workflow,
  lease_token=NULL,lease_until=NULL,error_code=NULL
 WHERE tenant_id=p_tenant AND id=p_job AND lease_token=p_token AND lease_until>clock_timestamp();
 IF NOT FOUND THEN RAISE EXCEPTION 'CAPTURE_LEASE_EXPIRED' USING ERRCODE='40001'; END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.close_capture_assessment(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.close_capture_assessment(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.close_capture_assessment(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
