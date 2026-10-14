-- +goose Up
-- Cursor discovery only; materialization must repeat locked authority and quota checks.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v26_render_derivative(p_after uuid)
RETURNS TABLE(tenant_id uuid,run_id uuid,project_id uuid,task_id uuid,result_id uuid,
 root_id uuid,root_sha text,source_attempt integer,source_fence bigint,srgb_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id,r.id,s.project_id,r.v26_derivative_task_id,s.result_id,
  s.root_id,s.root_sha,s.attempt,s.fence,r.config_snapshot->>'srgb_sha256'
 FROM photo.runs r
 CROSS JOIN LATERAL photo.v26_render_derivative_source(r.tenant_id,r.v26_derivative_task_id) s
 WHERE r.v26_derivative_task_id IS NOT NULL AND r.kind='preview' AND r.status='draft'
 AND NOT r.graph_sealed AND NOT r.cancel_requested AND (p_after IS NULL OR r.id>p_after)
 AND r.config_snapshot->>'source_result_id'=s.result_id::text
 AND r.config_snapshot->>'source_task_id'=r.v26_derivative_task_id::text
 AND r.config_snapshot->>'source_run_id'=s.run_id::text
 AND r.config_snapshot->>'project_id'=s.project_id::text
 AND r.config_snapshot->>'source_attempt'=s.attempt::text AND r.config_snapshot->>'source_fence'=s.fence::text
 AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
 AND r.config_snapshot->>'srgb_sha256' ~ '^[0-9a-f]{64}$'
 AND r.config_snapshot->'variants'=jsonb_build_array('preview1600','preview2560')
 ORDER BY r.id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.next_v26_render_derivative(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v26_render_derivative(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.next_v26_render_derivative(uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
