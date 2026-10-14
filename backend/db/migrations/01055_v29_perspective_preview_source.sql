-- +goose Up
-- Read-only scheduler observation. This does not authorize preview dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_preview_source(p_tenant uuid,p_task uuid)
RETURNS TABLE(project_id uuid,run_id uuid,workflow_id uuid,result_id uuid,
 root_id uuid,root_sha text,attempt integer,fence bigint,actor_id uuid,auth_session_id uuid,
 source_version_id uuid,source_id uuid,source_sha text,canvas_hash text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.project_id,r.id,w.id,b.result_id,b.artifact_id,b.sha256,t.attempt,t.fencing_token,
  (plan.body->>'actor_id')::uuid,(plan.body->>'auth_session_id')::uuid,
  (params.body->'base'->>'asset_version_id')::uuid,b.source_artifact_id,b.source_sha256,b.canvas_hash
 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id AND project.archived_at IS NULL
 CROSS JOIN LATERAL (SELECT r.config_snapshot->'v29_perspective_plan' AS body) plan
 CROSS JOIN LATERAL (SELECT (plan.body->>'params_jcs')::jsonb AS body) params
 CROSS JOIN LATERAL photo.v29_adopted_perspective_binding(p_tenant,w.project_id,
  (params.body->'base'->>'asset_version_id')::uuid,p_task,
  (plan.body->>'actor_id')::uuid,(plan.body->>'auth_session_id')::uuid) b
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND t.tenant_id=p_tenant AND t.id=p_task AND r.status IN('waiting_review','completed')
  AND NOT r.cancel_requested AND r.kind='v2' AND r.graph_sealed
  AND plan.body->>'tenant_id'=p_tenant::text AND plan.body->>'project_id'=w.project_id::text
  AND plan.body->>'run_id'=r.id::text AND plan.body->>'workflow_id'=w.id::text
  AND plan.body->>'task_id'=t.id::text AND plan.body->>'snapshot_id'=r.input_snapshot_id::text;
$$;
ALTER FUNCTION photo.v29_perspective_preview_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_preview_source(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_preview_source(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
