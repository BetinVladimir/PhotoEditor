-- +goose Up
-- Empty string means no multipart was ever bound. NULL means no authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_drain_multipart(p_tenant uuid,p_upload uuid,p_bucket text,p_key text) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(x.multipart_id,'') FROM photo.v30_archive_uploads x
 WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.cleanup_token IS NOT NULL
 AND photo.v30_archive_cleanup_authorized(p_tenant,p_upload,x.cleanup_token,p_bucket,p_key)
$$;
ALTER FUNCTION photo.v30_archive_drain_multipart(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_drain_multipart(uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_archive_drain_multipart(uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
