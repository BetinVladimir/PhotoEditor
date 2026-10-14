-- +goose Up
-- Necessary ledger fence, deliberately not an attestation of remote IO drain.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_cleanup_authorized(p_tenant uuid,p_upload uuid,p_token uuid,p_bucket text,p_key text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_token IS NOT NULL AND EXISTS(
 SELECT 1 FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.cleanup_token=p_token AND x.cleanup_lease_until>clock_timestamp()
 AND x.state='aborting' AND x.error_code IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED') AND t.status='active'
 AND x.bucket=p_bucket AND x.object_key=p_key AND x.object_key='tenants/'||p_tenant||'/interchange-uploads/'||p_upload||'/archive.zip'
 AND x.cleanup_not_before<=clock_timestamp() AND x.expires_at<=clock_timestamp()
 AND (x.parts_authorized_until IS NULL OR x.parts_authorized_until<=clock_timestamp()) AND x.completion_lease_until IS NULL
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 );
$$;
ALTER FUNCTION photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
