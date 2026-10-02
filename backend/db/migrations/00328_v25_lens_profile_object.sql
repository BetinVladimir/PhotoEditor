-- +goose Up
-- Object lookup grants no qualification: compiler must verify trusted pins.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_profile_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 8388608
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_profile_object(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_profile_object(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_profile_object(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
