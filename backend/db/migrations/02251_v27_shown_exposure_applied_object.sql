-- +goose Up
-- Exact-version storage evidence is available only through authenticated retained result projection.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_applied_object(p_tenant uuid,p_target uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT 'WorkingImage'::text,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.semantic_shown_applied_image(p_tenant,p_target,p_actor,p_session) proof
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=proof.artifact_id AND a.sha256=proof.sha256
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 1048576
 AND a.media_type='application/json' AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_shown_applied_object(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_applied_object(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_shown_applied_object(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
