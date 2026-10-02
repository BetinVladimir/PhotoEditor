-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_capture_assessment_commit(p_tenant uuid,p_job uuid,p_token uuid,
 p_run uuid,p_workflow uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.capture_assessment_jobs%ROWTYPE;
BEGIN
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO j FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'leased' OR j.lease_token IS DISTINCT FROM p_token
  OR j.lease_until<=clock_timestamp() THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.id=p_workflow AND w.run_id=p_run AND w.project_id=j.project_id
  AND w.workflow_type='capture' AND w.renderer_version='v2.2') THEN RETURN false; END IF;
 PERFORM 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2'
  AND status='running' AND NOT graph_sealed AND NOT cancel_requested FOR UPDATE;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_capture_assessment_commit(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_capture_assessment_commit(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_capture_assessment_commit(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.close_capture_assessment(p_tenant uuid,p_job uuid,p_token uuid,
 p_workflow uuid,p_params_jcs text,p_context_jcs text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.capture_assessment_jobs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 t photo.tasks%ROWTYPE; params jsonb; context jsonb; prep uuid; expansion uuid;
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
 IF NOT photo.lock_capture_assessment_commit(p_tenant,p_job,p_token,w.run_id,w.id) THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id)<>2 THEN RETURN false; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id AND operation='capture.assess' FOR UPDATE;
 IF NOT FOUND OR t.operation<>'capture.assess' OR t.operation_version<>'1' THEN RETURN false; END IF;
 params:=p_params_jcs::jsonb;context:=p_context_jcs::jsonb;
 IF params->>'schema_version' IS DISTINCT FROM '1' OR context->>'schema_version' IS DISTINCT FROM '1'
  OR (params->'binding'->>'asset_version_id')::uuid IS DISTINCT FROM j.asset_version_id
  OR (context->>'session_id')::uuid IS DISTINCT FROM j.session_id
  OR params->'policy' IS DISTINCT FROM j.capture_policy
  OR NOT EXISTS(SELECT 1 FROM photo.asset_versions v WHERE v.tenant_id=p_tenant
   AND v.id=j.asset_version_id AND v.master_artifact_id=(params->'binding'->'source'->>'artifact_id')::uuid
   AND v.content_hash=params->'binding'->'source'->>'sha256')
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
    AND a.sha256=encode(sha256(convert_to(p_params_jcs,'UTF8')),'hex'))
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id
    AND i.artifact_id=(params->'context'->>'artifact_id')::uuid
    AND i.sha256=params->'context'->>'sha256'
    AND i.sha256=encode(sha256(convert_to(p_context_jcs,'UTF8')),'hex')) THEN RETURN false; END IF;
 SELECT task_id INTO prep FROM photo.capture_prepared_source(p_tenant,w.run_id);
 IF prep IS NULL OR NOT photo.v2_input_closure_ready(t.id)
  OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
  OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation
   AND op.version=t.operation_version AND op.v2_enabled AND op.backends ? (t.execution_profile->>'backend'))
  OR NOT EXISTS(SELECT 1 FROM photo.capture_prepared_artifact(p_tenant,w.run_id,
   (params->'proxy'->>'artifact_id')::uuid,params->'proxy'->>'sha256','working_image'))
  OR NOT EXISTS(SELECT 1 FROM photo.capture_prepared_artifact(p_tenant,w.run_id,
   (params->'faces'->>'artifact_id')::uuid,params->'faces'->>'sha256','faces'))
  OR (SELECT count(*) FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=w.run_id)<>1
  OR NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=w.run_id
   AND prerequisite_id=prep AND dependent_id=t.id AND condition='success') THEN RETURN false; END IF;
 SELECT id INTO expansion FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=w.run_id
  AND kind='v2.capture.assess/1' AND parent_task_id=prep AND state='open' AND expected_count=1 FOR UPDATE;
 IF NOT FOUND OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant
  AND run_id=w.run_id AND id<>expansion AND state<>'closed') THEN RETURN false; END IF;
 UPDATE photo.graph_expansions SET state='closed',cursor='1',expansion_token=NULL,expansion_lease_until=NULL
  WHERE tenant_id=p_tenant AND id=expansion;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=w.run_id
  AND status='running' AND NOT graph_sealed AND NOT cancel_requested AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'CAPTURE_GRAPH_SEAL_RACE' USING ERRCODE='40001'; END IF;
 IF NOT photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) THEN
  RAISE EXCEPTION 'CAPTURE_ASSESS_ACTIVATION_FAILED' USING ERRCODE='23514'; END IF;
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
