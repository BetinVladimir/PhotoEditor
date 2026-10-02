-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.capture_preparation_progress(p_tenant uuid,p_job uuid,p_token uuid)
RETURNS TABLE(workflow_id uuid,run_id uuid,phase text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.id,r.id,CASE
  WHEN r.cancel_requested OR t.cancel_requested OR r.status IN('cancelling','cancelled') THEN 'cancelled'
  WHEN r.status='failed' OR t.state='failed' THEN 'failed'
  WHEN r.status='draft' THEN 'draft'
  WHEN EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN 'ready'
  ELSE 'pending' END
 FROM photo.capture_assessment_jobs j JOIN photo.tenants tenant ON tenant.id=j.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=j.tenant_id AND w.project_id=j.project_id AND w.id=j.preparation_workflow_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.operation='capture.prepare' AND t.operation_version='1'
 WHERE j.tenant_id=p_tenant AND j.id=p_job AND j.state='leased' AND j.lease_token=p_token
  AND j.lease_until>clock_timestamp() AND w.workflow_type='capture' AND w.renderer_version='v2.2';
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_preparation_progress(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_preparation_progress(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_preparation_progress(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
