-- +goose Up
-- Capture closure registration must acquire its exclusive lifecycle lock before
-- generic registration; upgrading shared tenant locks can deadlock two jobs.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_capture_assessment_commit(p_tenant uuid,p_job uuid,p_token uuid,
 p_run uuid,p_workflow uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.capture_assessment_jobs%ROWTYPE;
BEGIN
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO j FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'leased' OR j.lease_token IS DISTINCT FROM p_token
  OR j.lease_until<=clock_timestamp() THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.id=p_workflow AND w.run_id=p_run AND w.project_id=j.project_id
  AND w.workflow_type='capture' AND w.renderer_version='v2.2') THEN RETURN false; END IF;
 PERFORM 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2'
  AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR UPDATE;
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
