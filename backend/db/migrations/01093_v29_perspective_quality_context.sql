-- +goose Up
-- Read-only exact report context; no quality acceptance or version mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_quality_context(p_tenant uuid,p_run uuid)
RETURNS TABLE(task_id uuid,result_id uuid,attempt integer,fence bigint,
 params_id uuid,params_sha text,report_id uuid,report_sha text,target_id uuid,target_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT o.task_id,o.result_id,o.attempt,o.fence,d.artifact_id,d.sha256,o.artifact_id,o.sha256,
 (o.params::jsonb->'target'->>'artifact_id')::uuid,o.params::jsonb->'target'->>'sha256'
 FROM photo.v29_perspective_quality_result(p_tenant,p_run) o
 JOIN photo.backend_documents d ON d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=o.task_id
  AND d.purpose='task_params' AND d.state='ready'
 WHERE o.output_role='quality_report';
$$;
ALTER FUNCTION photo.v29_perspective_quality_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_quality_context(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_quality_context(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
