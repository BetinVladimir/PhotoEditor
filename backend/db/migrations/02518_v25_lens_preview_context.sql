-- +goose Up
-- Scoped context for independent byte validation before receipt transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_preview_context(p_tenant uuid,p_run uuid,p_variant text)
RETURNS TABLE(task_id uuid,result_id uuid,attempt integer,fence bigint,params_id uuid,params_sha text,manifest_id uuid,manifest_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT o.task_id,o.result_id,o.attempt,o.fence,d.artifact_id,d.sha256,o.artifact_id,o.sha256
 FROM photo.v25_lens_preview_result(p_tenant,p_run,p_variant) o
 JOIN photo.backend_documents d ON d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=o.task_id AND d.purpose='task_params' AND d.state='ready'
 WHERE o.output_role='derivative_manifest';
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_preview_context(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_preview_context(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_preview_context(uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
