-- +goose Up
-- Inherited NPY preparation metadata; caller follows only accepted WorkingImage refs.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_exposure_presentation_input(p_tenant uuid,p_target uuid,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.semantic_exposure_presentation_source(p_tenant,p_target) source
 JOIN photo.v2_task_inputs i ON i.tenant_id=p_tenant AND i.task_id=source.task_id AND i.project_id=source.project_id AND i.artifact_id=p_artifact
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND a.media_type IN('application/x-npy','application/octet-stream');
$$;
ALTER FUNCTION photo.semantic_exposure_presentation_input(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_exposure_presentation_input(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_exposure_presentation_input(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
