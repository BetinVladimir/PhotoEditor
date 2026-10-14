-- +goose Up
-- Preserve Render source behavior; add only completed accepted semantic exposure.
-- +goose StatementBegin
ALTER FUNCTION photo.v26_render_derivative_source(uuid,uuid) RENAME TO v26_render_derivative_source_before_exposure;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_source_before_exposure(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v26_render_derivative_source(p_tenant uuid,p_task uuid)
RETURNS TABLE(project_id uuid,run_id uuid,result_id uuid,root_id uuid,root_sha text,attempt integer,fence bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT * FROM photo.v26_render_derivative_source_before_exposure(p_tenant,p_task)
 UNION ALL
 SELECT s.project_id,s.child_run_id,s.result_id,s.root_artifact_id,s.root_sha256,s.attempt,s.fence
 FROM photo.semantic_targets target
 CROSS JOIN LATERAL photo.semantic_exposure_presentation_source(target.tenant_id,target.id) s
 WHERE target.tenant_id=p_tenant AND s.task_id=p_task;
$$;
ALTER FUNCTION photo.v26_render_derivative_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_source(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v26_render_derivative_source(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
