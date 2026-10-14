-- +goose Up
-- Reuse catalog provenance; this is original resolution, never approval of GPS
-- or adoption of a normalized metadata document. Canvas bytes checked by caller.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_metadata_original(p_tenant uuid,p_project uuid,
 p_version uuid,p_source uuid,p_sha text,p_actor uuid,p_session uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT original.* FROM photo.v25_lens_original_source(
 p_tenant,p_project,p_version,p_source,p_sha,p_actor,p_session) original
 WHERE nullif(current_setting('app.tenant_id',true),'')::uuid=p_tenant
 AND EXISTS(SELECT 1 FROM photo.projects project WHERE project.tenant_id=p_tenant
 AND project.id=p_project AND project.archived_at IS NULL AND project.download_allowed);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_metadata_original(uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_metadata_original(uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_metadata_original(uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
