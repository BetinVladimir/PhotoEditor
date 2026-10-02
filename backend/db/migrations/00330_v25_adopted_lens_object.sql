-- +goose Up
-- Exact adopted root object only; no nested or generic project grants.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_adopted_lens_object(p_tenant uuid,p_project uuid,p_version uuid,
 p_task uuid,p_actor uuid,p_session uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v25_adopted_lens_binding(p_tenant,p_project,p_version,p_task,p_actor,p_session) b
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=b.artifact_id AND a.sha256=b.sha256
 WHERE b.artifact_id=p_artifact AND b.sha256=p_sha
  AND coalesce(p_sha,'') ~ '^[0-9a-f]{64}$' AND a.bytes BETWEEN 2 AND 1048576;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_adopted_lens_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_adopted_lens_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_adopted_lens_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
