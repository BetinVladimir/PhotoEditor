-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.seal_capture_preparation(p_tenant uuid,p_job uuid,p_token uuid,
 p_workflow uuid,p_params_jcs text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE job photo.capture_assessment_jobs%ROWTYPE; workflow photo.v2_workflows%ROWTYPE;
 task photo.tasks%ROWTYPE; params jsonb;
BEGIN
 IF p_params_jcs IS NULL OR octet_length(p_params_jcs)>1048576 THEN RETURN false; END IF;
 IF NOT photo.bind_capture_preparation(p_tenant,p_job,p_token,p_workflow) THEN RETURN false; END IF;
 SELECT * INTO job FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 IF NOT FOUND OR workflow.project_id<>job.project_id OR workflow.workflow_type<>'capture' THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=workflow.run_id)<>1 THEN RETURN false; END IF;
 SELECT * INTO task FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=workflow.run_id FOR UPDATE;
 IF NOT FOUND OR task.operation<>'capture.prepare' OR task.operation_version<>'1'
  OR task.node_key<>'capture.prepare:0' OR task.resource_class<>'gpu' OR task.state<>'blocked'
  OR task.attempt<>0 OR task.cancel_requested THEN RETURN false; END IF;
 params:=p_params_jcs::jsonb;
 IF params->>'schema_version' IS DISTINCT FROM '1'
  OR (params->'binding'->>'asset_version_id')::uuid IS DISTINCT FROM job.asset_version_id
  OR params->'calibration' IS DISTINCT FROM job.capture_policy->'calibration'
  OR NOT EXISTS(SELECT 1 FROM photo.asset_versions v WHERE v.tenant_id=p_tenant
   AND v.id=job.asset_version_id AND v.master_artifact_id=(params->'binding'->'source'->>'artifact_id')::uuid
   AND v.content_hash=params->'binding'->'source'->>'sha256')
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=task.params_artifact_id
   AND a.sha256=encode(sha256(convert_to(p_params_jcs,'UTF8')),'hex'))
 THEN RETURN false; END IF;
 IF NOT photo.seal_v2_graph(p_tenant,workflow.run_id) THEN RETURN false; END IF;
 IF job.lease_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'CAPTURE_PREPARATION_LEASE_EXPIRED' USING ERRCODE='40001'; END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_capture_preparation(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_capture_preparation(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_capture_preparation(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
