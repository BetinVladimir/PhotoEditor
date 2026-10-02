-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_adoption_params(p_tenant uuid,p_task uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='optical.lens'
 AND t.operation_version='1' AND a.state='ready'
 AND EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task))
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id
 AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_adoption_params(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_adoption_params(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_adoption_params(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
