-- +goose Up
ALTER TABLE photo.capture_assessment_jobs ADD COLUMN preparation_workflow_id uuid;
ALTER TABLE photo.capture_assessment_jobs ADD CONSTRAINT capture_preparation_workflow_fk
 FOREIGN KEY(tenant_id,project_id,preparation_workflow_id) REFERENCES photo.v2_workflows(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE UNIQUE INDEX capture_preparation_workflow_unique ON photo.capture_assessment_jobs(tenant_id,preparation_workflow_id)
 WHERE preparation_workflow_id IS NOT NULL;
-- +goose StatementBegin
CREATE FUNCTION photo.bind_capture_preparation(p_tenant uuid,p_job uuid,p_token uuid,p_workflow uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE job photo.capture_assessment_jobs%ROWTYPE; workflow photo.v2_workflows%ROWTYPE; run photo.runs%ROWTYPE;
BEGIN
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO job FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR job.state<>'leased' OR job.lease_token IS DISTINCT FROM p_token
  OR job.lease_until<=clock_timestamp() THEN RETURN false; END IF;
 IF job.preparation_workflow_id IS NOT NULL THEN RETURN job.preparation_workflow_id=p_workflow; END IF;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 IF NOT FOUND OR workflow.workflow_type<>'capture' OR workflow.renderer_version<>'v2.2'
  OR workflow.project_id<>job.project_id THEN RETURN false; END IF;
 SELECT * INTO run FROM photo.runs WHERE tenant_id=p_tenant AND id=workflow.run_id FOR UPDATE;
 IF NOT FOUND OR run.kind<>'v2' OR run.status<>'draft' OR run.graph_sealed OR run.cancel_requested
  OR run.config_snapshot->'v24_capture'->'policy' IS DISTINCT FROM job.capture_policy
  OR (SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=run.input_snapshot_id)<>1
  OR NOT EXISTS(SELECT 1 FROM photo.snapshot_items WHERE tenant_id=p_tenant
   AND snapshot_id=run.input_snapshot_id AND asset_version_id=job.asset_version_id)
 THEN RETURN false; END IF;
 UPDATE photo.capture_assessment_jobs SET preparation_workflow_id=p_workflow WHERE tenant_id=p_tenant AND id=p_job;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.bind_capture_preparation(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_capture_preparation(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.bind_capture_preparation(uuid,uuid,uuid,uuid) TO photo_scheduler;
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
 IF j.preparation_workflow_id IS DISTINCT FROM p_workflow THEN RETURN false; END IF;
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

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
