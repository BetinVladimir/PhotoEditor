-- +goose Up
-- Actual adopted source, including receipt-authorized replacements; no child access.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_perspective_source_object(p_tenant uuid,p_project uuid,p_version uuid,
 p_task uuid,p_actor uuid,p_session uuid,p_root uuid,p_root_sha text,p_source uuid,p_source_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v29_adopted_perspective_binding(p_tenant,p_project,p_version,p_task,p_actor,p_session) b
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=b.source_artifact_id AND a.sha256=b.source_sha256
 WHERE b.artifact_id=p_root AND b.sha256=p_root_sha
 AND b.source_artifact_id=p_source AND b.source_sha256=p_source_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND a.bytes BETWEEN 2 AND 65536 AND a.media_type='application/json';
$$;
ALTER FUNCTION photo.v29_adopted_perspective_source_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_perspective_source_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_adopted_perspective_source_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
