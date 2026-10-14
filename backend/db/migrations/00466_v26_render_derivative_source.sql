-- +goose Up
-- Read-only scheduler boundary; no graph mutation, policy activation or purge grants.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_derivative_source(p_tenant uuid,p_task uuid)
RETURNS TABLE(project_id uuid,run_id uuid,result_id uuid,root_id uuid,root_sha text,attempt integer,fence bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.project_id,t.run_id,z.id,a.id,a.sha256,t.attempt,t.fencing_token
 FROM photo.tasks t
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='v2' AND NOT r.cancel_requested
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.workflow_type='preview'
 JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.id::text=z.outputs->0->>'artifact_id'
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND NOT t.cancel_requested
 AND photo.v26_render_task_result_ready(p_tenant,p_task)
 AND EXISTS(SELECT 1 FROM photo.inbox i
  JOIN photo.run_events e ON e.tenant_id=i.tenant_id AND e.run_id=r.id AND e.type='v2.task.adopted'
  AND e.payload->>'task_id'=t.id::text AND e.payload->>'result_id'=z.id::text
  WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(t.id,z.id));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_derivative_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_source(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_derivative_source(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
